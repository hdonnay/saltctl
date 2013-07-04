package main

// vim: set noexpandtab :

import (
	"bytes"
	"code.google.com/p/go.crypto/ssh/terminal"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"time"
)

var user *string
var configDir *string
var timeOut *int64
var auth string
var serverUrl *url.URL
var jar *cookiejar.Jar

const (
	_          = iota
	E_NeedAuth = 1 << iota
	E_Oops
)

type internalError struct {
	Code uint32
}

func (i *internalError) Error() string {
	return fmt.Sprintf("Error: %x\n", i.Code)
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Commands:\n\n")
		fmt.Fprintf(os.Stderr, "  help\n    Print this help.\n\n")
		fmt.Fprintf(os.Stderr, "  e[xec] tgt fun [arg...]\n    Execute a function on target minions\n\n")
		fmt.Fprintf(os.Stderr, "  i[nfo] tgt\n    Return information on target minions\n\n")
		fmt.Fprintf(os.Stderr, "Notes:\n\n<confdir>/config is json that can be used to set options other than config dir.\nAll options must be strings.\n\n")
	}
	var err error
	var fi os.FileInfo
	var serverString *string
	jar, err = cookiejar.New(nil)
	// do flag parsing
	configDir = flag.String("c", fmt.Sprintf("/home/%s/.config/saltctl", os.Getenv("USER")), "directory to look for configs")
	user = flag.String("u", os.Getenv("USER"), "username to authenticate with")
	serverString = flag.String("s", "https://salt:8000", "server url to talk to")
	timeOut = flag.Int64("t", 30, "Time in sec to wait for response")
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
			case "timeout":
				f := flag.Lookup("t")
				if f.Value.String() == f.DefValue {
					x, _ := strconv.ParseInt(v, 10, 64)
					timeOut = &x
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
	//fmt.Fprintf(os.Stderr, "info: token: %s\n", auth)
}

type lowstate struct {
	Client string   `json:"client"`
	Target string   `json:"tgt"`
	Fun    string   `json:"fun"`
	Arg    []string `json:"arg"`
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

func watch(c chan map[string]interface{}, id string, last bool) {
	var req *http.Request
	client := &http.Client{Jar: jar}
	var ret map[string][]map[string]interface{}
	req = mkReq("GET", fmt.Sprintf("jobs/%s", id), nil)
	for _ = range time.Tick(2 * time.Second) {
		res, err := client.Do(req)
		if err != nil {
			break
		}
		d := json.NewDecoder(res.Body)
		d.Decode(&ret)
		if len(ret["return"][0]) > 0 {
			for _, x := range ret["return"] {
				c <- x
			}
			break
		}
	}
	if last {
		close(c)
	}
}

func async(l []lowstate) (chan map[string]interface{}, error) {
	var err error
	var req *http.Request
	var res *http.Response
	ret := make(chan map[string]interface{})
	c := &http.Client{Jar: jar}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}
	//_, err = io.Copy(os.Stdout, bytes.NewReader(b))
	req = mkReq("POST", "", &b)
	res, err = c.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusUnauthorized {
		return nil, &internalError{E_NeedAuth}
	}
	if err != nil {
		return nil, err
	}
	go func() {
		var body map[string][]map[string]interface{}
		d := json.NewDecoder(res.Body)
		d.Decode(&body)
		for k, v := range body["return"] {
			if v["jid"] == nil {
				ret <- v
				if len(body["return"]) == 1 {
					close(ret)
				}
			} else {
				go watch(ret, v["jid"].(string), (len(body["return"]) == (k + 1)))
			}
		}
	}()
	return ret, nil
}

func main() {
	login(false)
	args := flag.Args()
	var err error
	var r chan map[string]interface{}
	switch flag.Arg(0) {
	case "exec", "e":
		r, err = async([]lowstate{lowstate{"local_async", args[1], args[2], args[3:]}})
		if err != nil {
			login(true)
			r, _ = async([]lowstate{lowstate{"local_async", args[1], args[2], args[3:]}})
		}
	case "info", "i":
		r, err = async([]lowstate{lowstate{"local", args[1], "grains.items", []string{}}})
		if err != nil {
			login(true)
			r, _ = async([]lowstate{lowstate{"local", args[1], "grains.items", []string{}}})
		}
	}
	go func() {
		<-time.After(time.Duration(*timeOut) * time.Second)
		close(r)
	}()
	for ret := range r {
		prnt, _ := json.MarshalIndent(ret, "  ", "  ")
		bytes.NewReader(prnt).WriteTo(os.Stdout)
		fmt.Fprintf(os.Stdout, "\n")
	}
	leave()
}
