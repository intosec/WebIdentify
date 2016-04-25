// WebScanner project main.go
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type WebServerInfo struct {
	IP        string      `json:"ip"`
	Port      string      `json:"port"`
	Host      string      `json:"host"`
	Headers   http.Header `json:"headers"`
	IndexPage string      `json:"indexpage"`
	Error     string      `json:"error"`
}
type Target struct {
	IP   string
	Port string
	Host string
}

func HttpGet(t Target) WebServerInfo {
	url := ""
	ip := t.IP
	port := t.Port
	if port == "80" {
		url = "http://" + ip
	} else if port == "443" {
		url = "https://" + ip
	}
	info := WebServerInfo{}
	info.IP = t.IP
	info.Host = t.Host
	info.Port = t.Port
	tr := &http.Transport{
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true},
		DisableCompression: true,
	}
	client := &http.Client{
		Timeout:   time.Duration(int(time.Second) * *timeout),
		Transport: tr,
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		// handle error
		info.Error = err.Error()
	}
	req.Host = t.Host
	req.Header.Set("Connection", "Close")
	req.Header.Set("UserAgent", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")

	resp, err := client.Do(req)
	if err != nil {
		// handle error
		info.Error = err.Error()
	}
	if resp != nil {
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			// handle error
			info.Error = err.Error()
		}
		info.Headers = resp.Header
		info.IndexPage = string(body)
		if *verbose {
			fmt.Println(ip, resp.StatusCode)
		}
	} else {
		if *verbose {
			fmt.Println(ip, err.Error())
		}
	}
	return info
}

func ReadFile(filename string) []string {
	file, err := os.OpenFile(filename, os.O_RDONLY, os.ModeType)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	// A。 使用 bufio按行读取文件
	lines := []string{}
	br := bufio.NewReader(file)
	for {
		line, err := br.ReadString('\n')
		if err == io.EOF {
			break
		} else if line != "\n" && line != "\r\n" {
			line = strings.Replace(line, "\r", "", -1)
			line = strings.Replace(line, "\n", "", -1)
			lines = append(lines, line)
		}
	}
	return lines
	// B。 使用ioutil读取文件所有内容
	//	b, err := ioutil.ReadAll(file)
	//	if err != nil {
	//		panic(err)
	//	}
	//	fmt.Printf("%v", string(b))
}

var verbose *bool
var timeout *int

var lock sync.Mutex
var wg sync.WaitGroup
var ch chan Target
var result *os.File

func Worker(ch chan Target) {
	hasMore := true
	var t Target
	for hasMore {
		if t, hasMore = <-ch; hasMore {
			info := HttpGet(t)
			i, _ := json.Marshal(info)
			lock.Lock()
			result.Write(append(i, byte('\n')))
			result.Sync()
			lock.Unlock()
		}
	}
	wg.Done()
}

// 参数frm可以是文件或目录，不会给dst添加.zip扩展名
func Compressor(frm, dst string) error {
	buf := bytes.NewBuffer(make([]byte, 0, 10*1024*1024)) // 创建一个读写缓冲
	myzip := zip.NewWriter(buf)                           // 用压缩器包装该缓冲
	// 用Walk方法来将所有目录下的文件写入zip
	err := filepath.Walk(frm, func(path string, info os.FileInfo, err error) error {
		var file []byte
		if err != nil {
			return filepath.SkipDir
		}
		header, err := zip.FileInfoHeader(info) // 转换为zip格式的文件信息
		if err != nil {
			return filepath.SkipDir
		}
		header.Name, _ = filepath.Rel(filepath.Dir(frm), path)
		if !info.IsDir() {
			// 确定采用的压缩算法（这个是内建注册的deflate）
			header.Method = 8
			file, err = ioutil.ReadFile(path) // 获取文件内容
			if err != nil {
				return filepath.SkipDir
			}
		} else {
			file = nil
		}
		// 上面的部分如果出错都返回filepath.SkipDir
		// 下面的部分如果出错都直接返回该错误
		// 目的是尽可能的压缩目录下的文件，同时保证zip文件格式正确
		w, err := myzip.CreateHeader(header) // 创建一条记录并写入文件信息
		if err != nil {
			return err
		}
		_, err = w.Write(file) // 非目录文件会写入数据，目录不会写入数据
		if err != nil {        // 因为目录的内容可能会修改
			return err // 最关键的是我不知道咋获得目录文件的内容
		}
		return nil
	})
	if err != nil {
		return err
	}
	myzip.Close()               // 关闭压缩器，让压缩器缓冲中的数据写入buf
	file, err := os.Create(dst) // 建立zip文件
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = buf.WriteTo(file) // 将buf中的数据写入文件
	if err != nil {
		return err
	}
	return nil
}

func main() {
	verbose = flag.Bool("v", false, "Verbose on")
	timeout = flag.Int("w", 30, "Timeout")
	var compress = flag.Bool("c", false, "Compress result file and delete original file")
	var iplist = flag.String("i", "", "IP list file")
	var hostlist = flag.String("h", "", "Host list file")
	var output = flag.String("o", "", "Output file, scan result")
	var threads = flag.Int("t", 10, "Thread number")
	flag.Parse()
	if *iplist == "" || *output == "" {
		flag.Usage()
		os.Exit(0)
	}

	ch = make(chan Target)
	runtime.GOMAXPROCS(runtime.NumCPU())
	IPs := ReadFile(*iplist)
	var Hosts []string
	if *hostlist != "" {
		Hosts = ReadFile(*hostlist)
	}
	var err error
	result, err = os.OpenFile(*output, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	defer result.Close()

	fmt.Println("Scanning...")
	go func() {
		for _, ip := range IPs {
			val := strings.Split(ip, ":")
			if len(val) == 2 {
				t := &Target{}
				t.IP = val[0]
				t.Port = val[1]
				if len(Hosts) > 0 {
					for _, host := range Hosts {
						t.Host = host
						ch <- *t
					}
				}
				t.Host = t.IP
				ch <- *t
			}
		}
		defer close(ch)
	}()

	for i := 0; i < *threads; i++ {
		wg.Add(1)
		go Worker(ch)
	}
	wg.Wait()
	result.Close()

	if *compress {
		Compressor(*output, *output+".zip")
		//os.Remove(*output)
		fmt.Println("File compressed")
	}
	fmt.Println("Done!")
}
