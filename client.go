package main

// vim: set noexpandtab :

import (
	"bytes"
	"code.google.com/p/go.crypto/ssh/terminal"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	//"io/ioutil"
	"net/url"

//	"time"
//"regexp"
)

var user *string
var configDir *string
var auth string
var serverUrl *url.URL
var jar *cookiejar.Jar

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Commands:\n\n")
		fmt.Fprintf(os.Stderr, "  help\n    Print this help.\n\n")
		fmt.Fprintf(os.Stderr, "  e[xec] tgt fun [arg...]\n    Execute a function on target minions\n\n")
		fmt.Fprintf(os.Stderr, "Notes:\n\n<confdir>/config is json that can be used to set 'user' and 'server' options.\n\n")
	}
	var err error
	var fi os.FileInfo
	var serverString *string
	jar, err = cookiejar.New(nil)
	// do flag parsing
	configDir = flag.String("c", fmt.Sprintf("/home/%s/.config/saltctl", os.Getenv("USER")), "directory to look for configs")
	user = flag.String("u", os.Getenv("USER"), "username to authenticate with")
	serverString = flag.String("s", "https://salt:8000", "server url to talk to")
	flag.Parse()

	// load up config
	var cfg string = fmt.Sprintf("%s/config", *configDir)
	fi, err = os.Stat(cfg)
	if err == nil && !fi.IsDir() {
		f, err := os.Open(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		var c map[string]string
		dc := json.NewDecoder(f)
		dc.Decode(&c)
		f.Close()
		for k, v := range c {
			switch k {
			case "server":
				f := flag.Lookup("s")
				if f.Value.String() == f.DefValue {
					serverString = &v
				}
			case "user":
				f := flag.Lookup("u")
				if f.Value.String() == f.DefValue {
					user = &v
				}
			}
		}
	}

	// Do other init-ing
	serverUrl, err = url.Parse(*serverString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(1)
	}
	switch flag.Arg(0) {
	case "exec", "e":
		if len(flag.Args()) < 3 {
			flag.Usage()
			os.Exit(1)
		}
	case "info", "i":
		if len(flag.Args()) < 2 {
			flag.Usage()
			os.Exit(1)
		}
	case "help":
		fallthrough
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func leave() {
	var token string = fmt.Sprintf("%s/token", *configDir)
	f, err := os.Create(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	e := json.NewEncoder(f)
	e.Encode(auth)
	f.Close()
}

func login(reauth bool) {
	if reauth {
		type arg struct {
			Eauth    string `json:"eauth"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		type ret struct {
			Token  string
			Start  float64
			Expire float64
			User   string
			Eauth  string
			Perms  []string
		}
		var req *http.Request
		var res *http.Response
		c := &http.Client{Jar: jar}

		fmt.Fprintf(os.Stdout, "Password: ")
		pass, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintf(os.Stdout, "\n")

		// prompt for pass
		b, err := json.Marshal(&arg{"pam", *user, string(pass)})
		req = mkReq("POST", "login", &b)
		res, err = c.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		//fmt.Fprintf(os.Stderr, "debug: %+v\n", res)
		//fmt.Fprintf(os.Stderr, "debug: %+v\n", res.Body)
		d := json.NewDecoder(res.Body)
		var resp map[string][]ret
		err = d.Decode(&resp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %+v\n", err)
		}
		//fmt.Fprintf(os.Stderr, "debug: login response: %+v\n", resp)
		auth = resp["return"][0].Token
	} else {
		var token string = fmt.Sprintf("%s/token", *configDir)
		// Look for an existing token
		fi, err := os.Stat(*configDir)
		if err != nil || !fi.IsDir() {
			os.MkdirAll(*configDir, 0700)
		}
		_, err = os.Stat(token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "info: no token found\n")
		} else {
			f, err := os.Open(token)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			d := json.NewDecoder(f)
			d.Decode(&auth)
			f.Close()
		}
		jar.SetCookies(serverUrl, []*http.Cookie{&http.Cookie{Name: "session_id", Value: auth}})
	}
	fmt.Fprintf(os.Stderr, "info: token: %s\n", auth)
}

type post struct {
	Client string   `json:"client"`
	Target string   `json:"tgt"`
	Fun    string   `json:"fun"`
	Arg    []string `json:"arg"`
}

func run(tgt, fun string, arg []string) error {
	var err error
	var req *http.Request
	var res *http.Response
	c := &http.Client{Jar: jar}
	b, err := json.Marshal([]post{post{"local_async", tgt, fun, arg}})
	if err != nil {
		return err
	}
	//_, err = io.Copy(os.Stdout, bytes.NewReader(b))
	req = mkReq("POST", "", &b)
	res, err = c.Do(req)
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusUnauthorized {
		login(true)
		return run(tgt, fun, arg)
	}
	_, err = io.Copy(os.Stdout, res.Body)
	if err != nil {
		return err
	}
	return nil
}

func info(tgt string) error {
	var err error
	var req *http.Request
	var res *http.Response
	c := &http.Client{Jar: jar}
	b, err := json.Marshal([]post{post{"local", tgt, "grains.items", []string{}}})
	if err != nil {
		return err
	}
	req = mkReq("POST", "", &b)
	res, err = c.Do(req)
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusUnauthorized {
		login(true)
		return info(tgt)
	}
	_, err = io.Copy(os.Stdout, res.Body)
	if err != nil {
		return err
	}
	return nil
}

func mkReq(method, path string, body *[]byte) *http.Request {
	var err error
	var req *http.Request
	if body != nil {
		req, err = http.NewRequest(method, fmt.Sprintf("%s/%s", serverUrl.String(), path), bytes.NewReader(*body))
	} else {
		req, err = http.NewRequest(method, fmt.Sprintf("%s/%s", serverUrl.String(), path), nil)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Header", auth)
	//fmt.Fprintf(os.Stderr, "debug: made req: %v\n", req)
	return req
}

func main() {
	login(false)
	switch flag.Arg(0) {
	case "exec", "e":
		a := flag.Args()
		run(a[1], a[2], a[3:])
	case "info", "i":
		for _, m := range flag.Args()[1:] {
			info(m)
		}
	}
	fmt.Fprintf(os.Stdout, "\n")
	leave()
}
