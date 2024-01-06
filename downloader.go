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

var (
	client = &http.Client{
		Timeout: time.Millisecond * 60000,
	}
)

type fileInfo struct {
	name string
	url  string
	key  []byte
	iv   []byte
}

type Downloader struct {
	m3u8       *fileInfo   // m3u8下载地址
	dir        string      // 保存目录
	cookie     string      // 自定义下载 Cookie
	referer    string      // 自定义下载 Referer
	goroutines int         // 下载线程数
	force      bool        // 是否强制重新下载
	ts         []*fileInfo // TS文件列表
	name       string      // 下面文件名称
}

func (downloader *Downloader) getRequestUrl(sub string) string {
	if strings.HasPrefix(sub, "/") {
		u, _ := url.Parse(downloader.m3u8.url)
		p := ""
		if u.Port() != "" {
			p = ":" + u.Port()
		}
		return u.Scheme + "://" + u.Host + p + sub
	} else {
		return string(downloader.m3u8.url[:strings.LastIndex(downloader.m3u8.url, "/")]) + "/" + sub
	}
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
		req.Header.Set("Referer", downloader.m3u8.url[:strings.LastIndex(downloader.m3u8.url, "/")])
	} else {
		req.Header.Set("Referer", downloader.referer)
	}
	return req, nil
}

func (downloader *Downloader) printDownloaderDetails() {
	f := "%-10s %s\n"
	fmt.Printf(f, "Url", downloader.m3u8.url)
	fmt.Printf(f, "Cookie", downloader.cookie)
	fmt.Printf(f, "Referer", downloader.referer)
	fmt.Printf(f, "Goroutines", strconv.Itoa(downloader.goroutines))
	fmt.Printf(f, "Force", strconv.FormatBool(downloader.force))
	fmt.Printf(f, "Directory", downloader.dir[:strings.LastIndex(downloader.dir, string(os.PathSeparator))])
	fmt.Printf(f, "File Name", downloader.name)
}

// Download 下载m3u8文件以及解析后的所有ts文件
func (downloader *Downloader) Download() {
	downloader.printDownloaderDetails()
	if downloader.m3u8.url == "" {
		ShowProgressBar("Failed", 0, "Url of m3u8 file not found")
		fmt.Println()
		return
	}

	if downloader.force {
		// 删除下载文件夹内所有文件
		err := os.RemoveAll(downloader.dir)
		if err != nil {
			ShowProgressBar("Failed", 0, fmt.Sprintf("Can not delete directory: %s", downloader.dir))
			fmt.Println()
			return
		}
	}
	if !fileExists(downloader.dir) {
		err := os.MkdirAll(downloader.dir, os.ModePerm)
		if err != nil {
			ShowProgressBar("Failed", 0, fmt.Sprintf("Can not create directory: %s", downloader.dir))
			fmt.Println()
			return
		}
	}

	if fileExists(filepath.Join(downloader.dir, downloader.name)) {
		err := os.Remove(filepath.Join(downloader.dir, downloader.name))
		if err != nil {
			ShowProgressBar("Failed", 0, fmt.Sprintf("Can not delete file: %s", filepath.Join(downloader.dir, downloader.name)))
			fmt.Println()
			return
		}
	}

	// 下载m3u8文件
	ShowProgressBar("Downloading", 0, downloader.name)
	err := downloader.downloadM3u8File()
	if err != nil {
		_ = os.RemoveAll(downloader.dir)
		ShowProgressBar("Failed", 0, err.Error())
		fmt.Println()
		return
	}

	media, err := downloader.parseM3u8File()
	if err != nil {
		_ = os.RemoveAll(downloader.dir)
		ShowProgressBar("Failed", 0, err.Error())
		fmt.Println()
		return
	}

	downloaded, err := downloader.downloadTsFiles(media)

	if err != nil {
		ShowProgressBar("Failed", 0, err.Error())
		fmt.Println()
		return
	}

	if downloaded != len(downloader.ts) {
		ShowProgressBar("Failed", float32(downloaded)/float32(len(downloader.ts)), "Some files failed to download, please try again")
		fmt.Println()
		return
	}
	downloader.appendTsFile()
	_ = os.Rename(filepath.Join(downloader.dir, downloader.name), filepath.Join(downloader.dir, "..", downloader.name))
	_ = os.RemoveAll(downloader.dir)
	ShowProgressBar("Completed", 1, downloader.name)
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
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode != 200 {
			return errors.New(fmt.Sprintf("Bad http status = %d, %s", resp.StatusCode, downloader.m3u8.url))
		}
		f, err := os.Create(filePath)
		if err != nil {
			return errors.New(fmt.Sprintf("Can not create file: %s", filePath))
		}
		defer func(f *os.File) {
			_ = f.Close()
		}(f)
		_, err = io.Copy(f, resp.Body)
		if err != nil {
			return errors.New(fmt.Sprintf("Can not write file: %s", filePath))
		}
	}
	return nil
}

func (downloader *Downloader) parseM3u8File() (*m3u8.MediaPlaylist, error) {
	m3u8File := filepath.Join(downloader.dir, downloader.m3u8.name)
	f, err := os.Open(m3u8File)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Can not open file: %s", m3u8File))
	}
	p, t, err := m3u8.DecodeFrom(bufio.NewReader(f), false)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Can not parse file: %s", m3u8File))
	}
	if t == m3u8.MEDIA {
		return p.(*m3u8.MediaPlaylist), nil
	} else {
		return nil, errors.New(fmt.Sprintf("Can not parse file: %s", m3u8File))
	}
}

func (downloader *Downloader) downloadTsFiles(media *m3u8.MediaPlaylist) (int, error) {
	var key []byte = nil
	var iv []byte = nil

	for i, segment := range media.Segments {
		if segment != nil && segment.URI != "" {
			url := segment.URI
			if !strings.HasPrefix(url, "http") {
				url = downloader.getRequestUrl(url)
			}
			if segment.Key != nil {
				iv = []byte(segment.Key.IV)
				var err error
				key, _, err = downloader.downloadKey(segment.Key)
				if err != nil {
					return 0, errors.New("Failed download key: " + segment.Key.URI)
				}
			}
			ts := fileInfo{
				name: fmt.Sprintf("%05d.ts", i+1),
				url:  url,
				key:  key,
				iv:   iv,
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
						ShowProgressBar("Downloading", float32(downloaded)/float32(len(downloader.ts)), f.name)
						for i := 5; i > 0; i-- {
							err := downloader.downloadTsFile(f)
							if err == nil {
								atomic.AddInt32(&downloaded, 1)
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

	return int(downloaded), nil
}

func (downloader *Downloader) appendTsFile() {
	files, _ := os.ReadDir(downloader.dir)
	tsFiles := make([]string, 0)
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".ts") {
			tsFiles = append(tsFiles, file.Name())
		}
	}
	sort.Strings(tsFiles)
	f, err := os.OpenFile(filepath.Join(downloader.dir, downloader.name), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		ShowProgressBar("Failed", 1, fmt.Sprintf("Can not open file: %s", filepath.Join(downloader.dir, downloader.name)))
		fmt.Println()
		return
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)
	for _, file := range tsFiles {
		ShowProgressBar("Merging", 1, file)
		b, err := os.ReadFile(filepath.Join(downloader.dir, file))
		if err == nil {
			_, _ = f.Write(b)
		}
	}
}

func (downloader *Downloader) downloadTsFile(ts *fileInfo) error {
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
		return errors.New(fmt.Sprintf("%s", ts.url))
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
		return errors.New(fmt.Sprintf("%s", ts.url))
	}
	if ts.key != nil {
		c, err := aesDecrypt(content, ts.key, ts.iv)
		if err != nil {
			return errors.New(fmt.Sprintf("%s", ts.url))
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
	if strings.ToUpper(key.Method) != "AES-128" {
		return nil, "", errors.New("Not supported encrypt method: " + key.Method)
	}

	u := key.URI
	if !strings.HasPrefix(u, "http") {
		u = downloader.getRequestUrl(u)
	}
	req, err := downloader.NewHttpRequest(u)
	if err != nil {
		return nil, u, nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, u, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != 200 {
		return nil, u, errors.New(fmt.Sprintf("Can not download key: %s", u))
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

var lock = sync.Mutex{}

func ShowProgressBar(title string, progress float32, msg string) {
	lock.Lock()
	defer lock.Unlock()
	fc := "\033[33m"
	if title == "Failed" {
		fc = "\033[31m"
	} else if title == "Completed" {
		fc = "\033[32m"
	}
	title = fmt.Sprintf("%v%s\033[39m", fc, title)
	w := defaultProgressBarWidth
	p := int(progress * float32(w))
	s := fmt.Sprintf("[%s] %s%*s %6.2f%% %s",
		title, strings.Repeat("=", p), w-p, "", progress*100, msg)
	fmt.Print("\r\033[0K")
	fmt.Print(s)
}

func NewDownloader(m3u8Url string, dir string, name string, cookie string, referer string, goroutines int, force bool) *Downloader {
	if name == "" {
		name = defaultName
	}
	d := Downloader{
		m3u8: &fileInfo{
			name: m3u8FileName,
			url:  m3u8Url,
		},
		dir:        dir,
		cookie:     cookie,
		goroutines: goroutines,
		force:      force,
		ts:         make([]*fileInfo, 0),
		name:       name,
		referer:    referer,
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
	if len(ivs) == 0 || len(ivs[0]) == 0 {
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
