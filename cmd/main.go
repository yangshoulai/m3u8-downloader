package main

import (
	"flag"
	m3u8_downloader "m3u8-downloader"
	"os"
	"runtime"
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
	m3u8Url := ""
	if len(args) > 0 {
		m3u8Url = args[0]
	}
	if dir == "" {
		d, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		dir = d
	}
	if goroutines <= 0 {
		goroutines = 2 * runtime.NumCPU()
	}
	downloader := m3u8_downloader.NewDownloader(m3u8Url, dir, name, cookie, referer, goroutines, force)
	downloader.Download()
}
