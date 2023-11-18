package main

import (
	"flag"
	"fmt"
	m3u8_downloader "m3u8-downloader"
	"os"
)

var (
	// 命令行参数
	tFlag = flag.Int("t", 16, "下载线程数")
	oFlag = flag.String("o", "movie.mp4", "自定义文件名")
	cFlag = flag.String("c", "", "自定义请求cookie")
	dFlag = flag.String("d", "", "文件保存的绝对路径(默认为当前路径)")
	fFLag = flag.Bool("f", false, "是否强制重新下载")
	rFlag = flag.String("r", "", "自定义请求Referer")
)

func main() {
	flag.Parse()
	goroutines := *tFlag
	name := *oFlag
	cookie := *cFlag
	dir := *dFlag
	force := *fFLag
	referer := *rFlag
	args := flag.Args()
	if len(args) == 0 || args[0] == "" {
		fmt.Println("未指定m3u8文件下载地址")
		fmt.Println("用法：./m3u8-downloader http(s)://host/xx/index.m3u8")
		return
	}
	m3u8Url := args[0]
	if dir == "" {
		d, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		dir = d
	}
	downloader := m3u8_downloader.NewDownloader(m3u8Url, dir, name)
	if cookie != "" {
		downloader.SetCookie(cookie)
	}
	if goroutines > 0 {
		downloader.SetGoroutines(goroutines)
	}
	downloader.SetForce(force)
	downloader.SetReferer(referer)
	downloader.Download()
}
