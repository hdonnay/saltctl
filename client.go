// +build go1.1

package main

// vim: set noexpandtab :

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	//"strconv"
	"time"
)

var user *string
var configDir *string
var timeOut *int64
var eauth *string
var reAuth *bool
var auth string
var serverUrl *url.URL
var jar *cookiejar.Jar
var prettyPrint bool = false

const (
	E_NeedAuth = 1 << iota
	E_Oops
)

type internalError struct {
	Code uint32
}

func (i *internalError) Error() string {
	return fmt.Sprintf("Error: %x\n", i.Code)
}

type lowstate struct {
	Client string   `json:"client"`
	Target string   `json:"tgt"`
	Fun    string   `json:"fun"`
	Arg    []string `json:"arg"`
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Commands:\n\n")
		fmt.Fprintf(os.Stderr, "  help\n    Print this help.\n\n")
		fmt.Fprintf(os.Stderr, "  e[xec] tgt fun [arg...]\n    Execute a function on target minions\n\n")
		fmt.Fprintf(os.Stderr, "  i[nfo] tgt\n    Return information on target minions\n\n")
		fmt.Fprintf(os.Stderr, "Notes:\n\n<confdir>/config is json that can be used to set options other than config dir.\n\n")
	}
	var err error
	var fi os.FileInfo
	var serverString *string
	jar, err = cookiejar.New(nil)
	// do flag parsing
	configDir = flag.String("c", fmt.Sprintf("/home/%s/.config/saltctl", os.Getenv("USER")), "directory to look for configs")
	user = flag.String("u", os.Getenv("USER"), "username to authenticate with")
	serverString = flag.String("s", "https://salt:8000", "server url to talk to")
	timeOut = flag.Int64("t", 60, "Time in sec to wait for response")
	eauth = flag.String("a", "pam", "eauth module to use")
	reAuth = flag.Bool("r", false, "force re-authentication")
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
		var c map[string]interface{}
		dc := json.NewDecoder(f)
		dc.Decode(&c)
		f.Close()
		for k, v := range c {
			switch k {
			case "server":
				f := flag.Lookup("s")
				if f.Value.String() == f.DefValue {
					*serverString = v.(string)
				}
			case "user":
				f := flag.Lookup("u")
				if f.Value.String() == f.DefValue {
					*user = v.(string)
				}
			case "timeout":
				f := flag.Lookup("t")
				if f.Value.String() == f.DefValue {
					*timeOut = int64(v.(float64))
				}
			case "eauth":
				f := flag.Lookup("a")
				if f.Value.String() == f.DefValue {
					*eauth = v.(string)
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

//Usage: %name %flags command [arg...]
func main() {
	login(*reAuth)
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
		if prettyPrint {
		} else {
			prnt, _ := json.MarshalIndent(ret, "  ", "  ")
			bytes.NewReader(prnt).WriteTo(os.Stdout)
			fmt.Fprintf(os.Stdout, "\n")
		}
	}
	leave()
}
