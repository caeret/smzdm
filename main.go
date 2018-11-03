package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Cookie string `json:"cookie"`
	SCKey  string `json:"sc_key"`
	FakeIP string `json:"fake_id"`
}

var (
	Cookie string
	SCKey  string
	FakeIP string

	client *http.Client
)

const (
	UserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/64.0.3282.119 Safari/537.36"
	Referer    = "https://www.smzdm.com"
	CheckInURL = "https://zhiyou.smzdm.com/user/checkin/jsonp_checkin"
)

func init() {
	rand.Seed(time.Now().Unix())

	jar, _ := cookiejar.New(nil)
	client = &http.Client{
		Jar:     jar,
		Timeout: time.Second * 30,
	}
}

func main() {
	configs := getConfigs()
	if len(configs) == 0 {
		fmt.Fprintln(os.Stderr, "no config provided.")
		os.Exit(1)
	}

	fns := []func() error{visit, checkIn}

	var exitCode int
	for i, config := range configs {
		log.SetPrefix(fmt.Sprintf("[%d]", i+1))
		Cookie = config.Cookie
		SCKey = config.SCKey
		FakeIP = config.FakeIP
		for _, fn := range fns {
			err := fn()
			if err != nil {
				log.Printf("fail to execute the script: %s.", err.Error())
				err = notify(fmt.Sprintf("签到失败: %s.", err.Error()))
				if err != nil {
					log.Printf("fail to send notify: %s.", err.Error())
				}
				exitCode = 1
			}
		}
	}

	os.Exit(exitCode)
}

func getConfigs() []Config {
	var configs []Config
	files, err := filepath.Glob("*.json")
	if err == nil {
		for _, file := range files {
			f, err := os.Open(file)
			if err == nil {
				var config Config
				err = json.NewDecoder(f).Decode(&config)
				if err == nil {
					configs = append(configs, config)
				}
				f.Close()
			}
		}
	}
	return configs
}

func prepareRequestHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", Referer)
	req.Header.Set("X-Forwarded-For", FakeIP)
}

func visit() error {
	log.Printf("visit the homepage: %s.", Referer)
	req, _ := http.NewRequest(http.MethodGet, Referer, nil)
	prepareRequestHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fail to send visit request: %s", err.Error())
	}
	resp.Body.Close()
	return nil
}

func checkIn() error {
	log.Printf("check in account: %s.", CheckInURL)
	u, err := url.Parse(CheckInURL)
	if err != nil {
		return fmt.Errorf("fail to do check in request: %s", err.Error())
	}
	q := u.Query()

	key := fmt.Sprintf("jQuery%d_%d", time.Now().Nanosecond(), time.Now().Unix()*1000+rand.Int63n(1000))
	q.Set("callback", key)
	q.Set("_", strconv.FormatInt(time.Now().Unix(), 10))
	u.RawQuery = q.Encode()
	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	prepareRequestHeaders(req)
	req.Header.Set("Cookie", Cookie)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fail to send sign in request: %s", err.Error())
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("fail to read data from check in response body: %s", err.Error())
	}

	var result struct {
		ErrorCode int    `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
		Data      struct {
			AddPoint   int             `json:"add_point"`
			CheckInNum json.RawMessage `json:"checkin_num"`
			Point      int             `json:"point"`
			Exp        int             `json:"exp"`
			Gold       int             `json:"gold"`
			Prestige   int             `json:"prestige"`
			Rank       int             `json:"rank"`
		} `json:"data"`
	}

	err = json.Unmarshal(b[len(key)+1:len(b)-1], &result)
	if err != nil {
		return fmt.Errorf("fail to unmarshal check in json: %s -> %s", string(b), err.Error())
	}

	if result.ErrorCode != 0 {
		return fmt.Errorf("签到失败: %s", string(b))
	}

	data := result.Data
	msg := fmt.Sprintf("连续 %s 天 / 积分 %d / 新增积分 %d / 经验 %d / 金币 %d / 威望 %d / 等级 %d.", string(bytes.Trim(data.CheckInNum, `"`)), data.Point, data.AddPoint, data.Exp, data.Gold, data.Prestige, data.Rank)
	log.Printf("result: %s", msg)
	return notify(msg)
}

func notify(msg string) error {
	if len(SCKey) == 0 {
		log.Println("keep silent, no notification will be sent.")
		return nil
	}
	u, err := url.Parse(fmt.Sprintf("http://sc.ftqq.com/%s.send", SCKey))
	if err != nil {
		return fmt.Errorf("fail to parse sc url: %s", err.Error())
	}
	q := u.Query()
	q.Set("text", "什么值得买签到")
	q.Set("desp", msg)
	u.RawQuery = q.Encode()
	req, _ := http.NewRequest(http.MethodPost, u.String(), nil)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fail to send notify request: %s", err.Error())
	}
	defer resp.Body.Close()

	var result struct {
		ErrNo  int    `json:"errno"`
		ErrMsg string `json:"errmsg"`
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("fail to read data from check in response: %s", err.Error())
	}
	err = json.Unmarshal(b, &result)
	if err != nil {
		return fmt.Errorf("fail to unmarshal notify json: %s -> %s", string(b), err.Error())
	}

	if result.ErrNo != 0 {
		return fmt.Errorf("fail to send notify: %s", string(b))
	}
	return nil
}
