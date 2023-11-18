package m3u8_downloader

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"github.com/grafov/m3u8"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultProgressBarWidth = 30
const defaultName = "movie.mp4"
const m3u8FileName = "m3u8"
const defaultSubDir = ".m3u8_downloader"

var (
	client = &http.Client{
		Timeout: time.Millisecond * 60000,
	}
)

type fileInfo struct {
	name string
	url  string
}

type Downloader struct {
	m3u8       *fileInfo   // m3u8下载地址
	dir        string      // 保存目录
	cookie     string      // 自定义下载 Cookie
	referer    string      // 自定义下载 Referer
	goroutines int         // 下载线程数
	force      bool        // 是否强制重新下载
	ts         []*fileInfo // TS文件列表
	host       string      // 下载主地址
	name       string      // 下面文件名称
}

func (d *Downloader) SetName(name string) {
	d.name = name
}

func (d *Downloader) SetCookie(cookie string) {
	d.cookie = cookie
}
func (d *Downloader) SetGoroutines(goroutines int) {
	d.goroutines = goroutines
}
func (d *Downloader) SetForce(f bool) {
	d.force = f
}
func (d *Downloader) SetReferer(r string) {
	d.referer = r
}

func getHost(u string) string {
	uri, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return uri.Scheme + "://" + uri.Host
}

func (downloader *Downloader) NewHttpRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if downloader.cookie != "" {
		req.Header.Set("Cookie", downloader.cookie)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.88 Safari/537.36")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9, en;q=0.8, de;q=0.7, *;q=0.5")
	if downloader.referer == "" {
		req.Header.Set("Referer", downloader.host)
	} else {
		req.Header.Set("Referer", downloader.referer)
	}
	return req, nil
}

// Download 下载m3u8文件以及解析后的所有ts文件
func (downloader *Downloader) Download() {
	if downloader.force {
		// 删除下载文件夹内所有文件
		err := os.RemoveAll(downloader.dir)
		if err != nil {
			panic(err)
		}
	}
	if !fileExists(downloader.dir) {
		err := os.MkdirAll(downloader.dir, os.ModePerm)
		if err != nil {
			panic(err)
		}
	}

	if fileExists(filepath.Join(downloader.dir, downloader.name)) {
		err := os.Remove(filepath.Join(downloader.dir, downloader.name))
		if err != nil {
			panic(err)
		}
	}

	// 下载m3u8文件
	err := downloader.downloadM3u8File()
	if err != nil {
		_ = os.RemoveAll(downloader.dir)
		showProgressBar("下载失败", 0, downloader.m3u8.url)
		return
	}
	showProgressBar("正在下载", 0, downloader.m3u8.url)
	media, err := downloader.parseM3u8File()
	if err != nil {
		_ = os.RemoveAll(downloader.dir)
		showProgressBar("解析失败", 0, downloader.m3u8.url)
		return
	}
	showProgressBar("正在下载", 0, downloader.m3u8.url)
	downloaded := downloader.downloadTsFiles(media)
	if downloaded != len(downloader.ts) {
		showProgressBar("下载失败", float32(downloaded)/float32(len(downloader.ts)), "部分文件下载失败，可尝试重新执行进行断点续传")
		return
	}
	downloader.appendTsFile()
	_ = os.Rename(filepath.Join(downloader.dir, downloader.name), filepath.Join(downloader.dir, "..", downloader.name))
	_ = os.RemoveAll(downloader.dir)
	showProgressBar("下载完成", 1, downloader.name)
	fmt.Println()
}

func (downloader *Downloader) downloadM3u8File() error {
	filePath := filepath.Join(downloader.dir, downloader.m3u8.name)
	if !fileExists(filePath) {
		req, err := downloader.NewHttpRequest(downloader.m3u8.url)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			return errors.New(fmt.Sprintf("无法下载m3u8文件，HTTP Status = %d", resp.StatusCode))
		}
		f, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer func(f *os.File) {
			_ = f.Close()
		}(f)
		_, err = io.Copy(f, resp.Body)
		if err != nil {
			return err
		}
	}
	return nil
}

func (downloader *Downloader) parseM3u8File() (*m3u8.MediaPlaylist, error) {
	m3u8File := filepath.Join(downloader.dir, downloader.m3u8.name)
	f, err := os.Open(m3u8File)
	if err != nil {
		return nil, err
	}
	p, t, err := m3u8.DecodeFrom(bufio.NewReader(f), false)
	if err != nil {
		return nil, err
	}
	if t == m3u8.MEDIA {
		return p.(*m3u8.MediaPlaylist), nil
	} else {
		return nil, errors.New("无法解析m3u8文件")
	}
}

func (downloader *Downloader) downloadTsFiles(media *m3u8.MediaPlaylist) int {
	key, u, err := downloader.downloadKey(media.Key)
	var iv []byte = nil
	if media.Key != nil {
		iv = []byte(media.Key.IV)
	}
	if err != nil {
		showProgressBar("下载失败", 0, u)
		return 0
	} else if len(key) > 0 {
		showProgressBar("正在下载", 0, u)
	}

	for i, segment := range media.Segments {
		if segment != nil && segment.URI != "" {
			ts := fileInfo{
				name: fmt.Sprintf("%05d.ts", i+1),
				url:  segment.URI,
			}
			downloader.ts = append(downloader.ts, &ts)
		}
	}

	ch := make(chan *fileInfo, len(downloader.ts))
	wg := sync.WaitGroup{}
	for _, t := range downloader.ts {
		ch <- t
	}
	wg.Add(len(downloader.ts))
	var downloaded int32 = 0
	for i := 0; i < downloader.goroutines; i++ {
		go func() {
			for {
				select {
				case f := <-ch:
					if f != nil {
						retries := 5
						for i := retries; i > 0; i-- {
							retries--
							err := downloader.downloadTsFile(f, key, iv)
							if err == nil {
								atomic.AddInt32(&downloaded, 1)
								showProgressBar("正在下载", float32(downloaded)/float32(len(downloader.ts)), f.url)
								break
							}
						}
						wg.Done()
					}
				}
			}
		}()
	}
	wg.Wait()
	close(ch)

	return int(downloaded)
}

func (downloader *Downloader) appendTsFile() {
	files, _ := os.ReadDir(downloader.dir)
	tsFiles := make([]string, 0)
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".ts") {
			tsFiles = append(tsFiles, filepath.Join(downloader.dir, file.Name()))
		}
	}
	sort.Slice(tsFiles, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})
	f, err := os.OpenFile(filepath.Join(downloader.dir, downloader.name), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)
	for _, file := range tsFiles {
		showProgressBar("正在合并", 1, file)
		b, err := os.ReadFile(file)
		if err == nil {
			_, _ = f.Write(b)
		}
	}
}

func (downloader *Downloader) downloadTsFile(ts *fileInfo, key []byte, iv []byte) error {
	filePath := downloader.dir + "/" + ts.name
	if fileExists(filePath) {
		return nil
	}
	req, err := downloader.NewHttpRequest(ts.url)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("%s 下载失败", ts.url))
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		return err
	}
	if len(content) == 0 || len(content) != contentLength {
		return errors.New(fmt.Sprintf("%s 下载失败", ts.url))
	}
	if key != nil {
		c, err := aesDecrypt(content, key, []byte(iv))
		if err != nil {
			return errors.New(fmt.Sprintf("%s 下载失败", ts.url))
		}
		content = c
	}
	for j := 0; j < len(content); j++ {
		if content[j] == uint8(71) {
			content = content[j:]
			break
		}
	}

	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)
	err = os.WriteFile(filePath, content, fs.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

func (downloader *Downloader) downloadKey(key *m3u8.Key) ([]byte, string, error) {
	if key == nil || key.URI == "" {
		return nil, "", nil
	}
	u := key.URI
	if !strings.HasPrefix(u, "http") {
		if strings.HasPrefix(u, "/") {
			u = downloader.host + u
		} else {
			u = downloader.host + "/" + u
		}
	}
	req, err := downloader.NewHttpRequest(u)
	if err != nil {
		return nil, u, nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, u, err
	}
	if resp.StatusCode != 200 {
		return nil, u, errors.New(fmt.Sprintf("%s 下载失败", u))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, u, err
	}
	return b, u, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

var preMsgLength = 0

func showProgressBar(title string, progress float32, msg string) {
	padding := preMsgLength - len(msg)
	preMsgLength = len(msg)
	if padding > 0 {
		msg = msg + strings.Repeat(" ", padding)
	}
	w := defaultProgressBarWidth
	p := int(progress * float32(w))
	s := fmt.Sprintf("[%s] %s%*s %6.2f%% %s",
		title, strings.Repeat("=", p), w-p, "", progress*100, msg)
	fmt.Print("\r" + s)
}

func NewDownloader(m3u8Url string, dir string, name string) *Downloader {
	d := Downloader{
		m3u8: &fileInfo{
			name: m3u8FileName,
			url:  m3u8Url,
		},
		dir:        dir,
		cookie:     "",
		goroutines: runtime.NumCPU(),
		force:      false,
		ts:         make([]*fileInfo, 0),
		host:       getHost(m3u8Url),
		name:       name,
	}
	d.dir = filepath.Join(d.dir, "."+d.name)
	return &d
}

func aesDecrypt(crypted, key []byte, ivs ...[]byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	var iv []byte
	if len(ivs) == 0 {
		iv = key
	} else {
		iv = ivs[0]
	}
	blockMode := cipher.NewCBCDecrypter(block, iv[:blockSize])
	origData := make([]byte, len(crypted))
	blockMode.CryptBlocks(origData, crypted)
	origData = pkcs7UnPadding(origData)
	return origData, nil
}
func pkcs7UnPadding(origData []byte) []byte {
	length := len(origData)
	unpadding := int(origData[length-1])
	return origData[:(length - unpadding)]
}
