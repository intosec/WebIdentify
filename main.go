// WebScanner project main.go
package main

import (
	//"archive/tar"
	"bufio"
	//"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
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

func main() {
	verbose = flag.Bool("v", false, "Verbose on")
	timeout = flag.Int("w", 10, "Timeout")
	//var compress = flag.Bool("c", false, "Compress result file and delete original file")
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

	//	if *compress {
	//		fw, err := os.Create(*output + ".tar.gz")
	//		if err != nil {
	//			fmt.Println(err.Error())
	//			os.Exit(1)
	//		}
	//		defer fw.Close()
	//		gw, _ := gzip.NewWriterLevel(fw, gzip.BestCompression)
	//		defer gw.Close()
	//		// tar write
	//		tw := tar.NewWriter(gw)
	//		defer tw.Close()
	//		fr, err := os.Open(*output)
	//		if err != nil {
	//			panic(err)
	//		}
	//		defer fr.Close()
	//		fi, err := fr.Stat()
	//		if err != nil {
	//			panic(err)
	//		}
	//		h := new(tar.Header)
	//		h.Name = fr.Name()
	//		h.Size = fi.Size()
	//		h.Mode = int64(fi.Mode())
	//		h.ModTime = fi.ModTime()
	//		// 写信息头
	//		err = tw.WriteHeader(h)
	//		if err != nil {
	//			panic(err)
	//		}
	//		// 写文件
	//		_, err = io.Copy(tw, fr)
	//		if err != nil {
	//			panic(err)
	//		}
	//		fr.Close()
	//		os.Remove(*output)
	//		fmt.Println("File compressed")
	//	}
	fmt.Println("Done!")
}
