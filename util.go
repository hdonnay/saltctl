package main

// vim: set noexpandtab :

import (
	"bytes"
	"code.google.com/p/go.crypto/ssh/terminal"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"time"
)

type resultName struct {
	Module  string
	Name    string
	NameArg string
	Func    string
}

func (n *resultName) String() string {
	if n.NameArg == n.Name {
		return fmt.Sprintf("%s.%s: %s", n.Module, n.Func, n.Name)
	} else {
		return fmt.Sprintf("%s.%s: %s (%s)", n.Module, n.Func, n.Name, n.NameArg)
	}
}

type result struct {
	RunNum  int64                  `json:"__run_num__"`
	Changes map[string]interface{} `json:"changes"`
	Comment string                 `json:""`
	Name    string                 `json:"name"`
	Result  bool                   `json:"result"`
}

func parseName(n string) *resultName {
	fmt.Fprintf(os.Stderr, "debug: parseName: %s given\n", n)
	s := regexp.MustCompile("_\\|-").Split(n, 4)
	fmt.Fprintf(os.Stderr, "debug: parseName: %d found\n", len(s))
	//return &resultName{s[0], s[1], s[2], s[3]}
	return nil
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
		b, err := json.Marshal(&arg{*eauth, *user, string(pass)})
		req = mkReq("POST", "login", &b)
		res, err = c.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if res.StatusCode == http.StatusUnauthorized {
			fmt.Fprintf(os.Stderr, "Authentication failed.\n")
			os.Exit(2)
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
