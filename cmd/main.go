package main

import (
	"flag"
	"fmt"
	m3u8_downloader "m3u8-downloader"
	"os"
)

var (
	// 命令行参数
	mFlag = flag.String("m", "", "m3u8下载地址(http(s)://url/xx/xx/index.m3u8)")
	tFlag = flag.Int("t", 16, "下载线程数(默认16)")
	oFlag = flag.String("o", "movie", "自定义文件名(默认为movie)不带后缀")
	cFlag = flag.String("c", "", "自定义请求cookie")
	dFlag = flag.String("d", "", "文件保存的绝对路径(默认为当前路径,建议默认值)")
)

func main() {
	flag.Parse()
	m3u8Url := *mFlag
	goroutines := *tFlag
	name := *oFlag
	cookie := *cFlag
	dir := *dFlag
	if m3u8Url == "" {
		fmt.Println("未指定m3u8文件下载地址")
		return
	}
	if dir == "" {
		d, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		dir = d
	}
	downloader := m3u8_downloader.NewDownloader(m3u8Url, dir)
	if name != "" {
		downloader.SetName(name)
	}
	if cookie != "" {
		downloader.SetCookie(cookie)
	}
	if goroutines > 0 {
		downloader.SetGoroutines(goroutines)
	}
	downloader.Download()
}
