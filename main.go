package main

import (
	"encoding/json"
	"flag"
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
	"strings"
	"time"
)

type Config struct {
	Username string `json:"username"`
	Password string `json:"password"`
	SCKey    string `json:"sc_key"`
}

var (
	username string
	password string
	SCKey    string

	client *http.Client
)

const (
	UserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/64.0.3282.119 Safari/537.36"
	Referer    = "https://www.smzdm.com"
	SignInURL  = "https://zhiyou.smzdm.com/user/login/ajax_check"
	CheckInURL = "https://zhiyou.smzdm.com/user/checkin/jsonp_checkin"
)

func init() {
	rand.Seed(time.Now().Unix())

	flag.StringVar(&username, "u", "", "specify the username.")
	flag.StringVar(&password, "p", "", "specify the password.")
	flag.StringVar(&SCKey, "k", "", "specify the push key provided by http://sc.ftqq.com/.")
	flag.Parse()

	jar, _ := cookiejar.New(nil)
	client = &http.Client{
		Jar:     jar,
		Timeout: time.Second * 10,
	}
}

func main() {
	configs := getConfigs()
	if len(configs) == 0 {
		fmt.Fprintln(os.Stderr, "no config provided.")
		os.Exit(1)
	}

	fns := []func() error{visit, signIn, checkIn}

	var exitCode int
	for i, config := range configs {
		log.SetPrefix(fmt.Sprintf("[%d]", i+1))
		username = config.Username
		password = config.Password
		SCKey = config.SCKey
		log.Printf("process account: %s.", username)
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
	if len(username) > 0 && len(password) > 0 {
		configs = append(configs, Config{username, password, SCKey})
	}
	return configs
}

func prepareRequestHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", Referer)
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

func signIn() error {
	log.Printf("sign in account: %s.", SignInURL)

	v := make(url.Values)
	v.Set("username", username)
	v.Set("password", password)
	req, _ := http.NewRequest(http.MethodPost, SignInURL, strings.NewReader(v.Encode()))
	prepareRequestHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fail to send sign in request: %s", err.Error())
	}
	defer resp.Body.Close()

	var result struct {
		ErrorCode int    `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
	}

	b, err := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(b, &result)
	if err != nil {
		return fmt.Errorf("fail to unmarshal sign in json: %s -> %s", string(b), err.Error())
	}

	if result.ErrorCode == 0 {
		log.Println("successfully signed account in.")
		return nil
	} else {
		return fmt.Errorf("failed to signed account in, error code: %d -> %s", result.ErrorCode, result.ErrorMsg)
	}
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
			AddPoint   int    `json:"add_point"`
			CheckInNum string `json:"checkin_num"`
			Point      int    `json:"point"`
			Exp        int    `json:"exp"`
			Gold       int    `json:"gold"`
			Prestige   int    `json:"prestige"`
			Rank       int    `json:"rank"`
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
	msg := fmt.Sprintf("连续 %s 天 / 积分 %d / 新增积分 %d / 经验 %d / 金币 %d / 威望 %d / 等级 %d.", data.CheckInNum, data.Point, data.AddPoint, data.Exp, data.Gold, data.Prestige, data.Rank)
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
	q.Set("text", "什么值得买签到: "+username)
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
