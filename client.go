// salt controller
//
//saltctl is a commandline tool to interface with salt-api(1)
//
//COMMANDS
//
//    help
//Print usage help.
//
//    e[xec] target fun [arg...]
//Run function 'fun' on 'target' minion(s) with the rest of the command line
//being used as arguments. saltctl will wait up to -t seconds for the function
//to finish and return data.
//
//    i[info] target
//Return infomation about 'target' minion(s)
//
//CONFIG FILE
//
//The file 'config' in directory -c can be used to store configuration values in
//json format.
//
//Supported settings are:
//
//"server": string containing URI (excluding path) to reach salt-api server.
//
//"user": string containing a username
//
//"timeout": integer specifying maximum bunber of seconds to wait for results from an async call.
//
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
		for k, v := range ret {
			fmt.Fprintf(os.Stdout, "%s:\n", parseName(k).String())
			prnt, _ := json.MarshalIndent(v, "  ", "  ")
			bytes.NewReader(prnt).WriteTo(os.Stdout)
			fmt.Fprintf(os.Stdout, "\n")
		}
	}
	leave()
}
