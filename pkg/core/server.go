package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"text/template"
	"time"

	iamTmpl "github.com/dciangot/sts-wire/pkg/template"
	"github.com/minio/minio-go/v6/pkg/credentials"
	"github.com/pkg/browser"
	"golang.org/x/oauth2"
)

// RCloneStruct ..
type RCloneStruct struct {
	Address  string
	Instance string
}

// IAMCreds ..
type IAMCreds struct {
	AccessToken  string
	RefreshToken string
}

// Server ..
type Server struct {
	Client     InitClientConfig
	Instance   string
	S3Endpoint string
	RemotePath string
	LocalPath  string
	Endpoint   string
	Response   ClientResponse
}

// Start ..
func (s *Server) Start() error {

	endpoint := s.Endpoint
	clientResponse := s.Response

	credsIAM := IAMCreds{}
	sigint := make(chan int, 1)

	//fmt.Println(clientResponse.ClientID)
	//fmt.Println(clientResponse.ClientSecret)

	ctx := context.Background()

	config := oauth2.Config{
		ClientID:     clientResponse.ClientID,
		ClientSecret: clientResponse.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  endpoint + "/authorize",
			TokenURL: endpoint + "/token",
		},
		RedirectURL: fmt.Sprintf("http://localhost:%d/oauth2/callback", s.Client.ClientConfig.Port),
		Scopes:      []string{"address", "phone", "openid", "email", "profile", "offline_access"},
	}

	state := RandomState()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		//log.Printf("%s %s", r.Method, r.RequestURI)
		if r.RequestURI != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, config.AuthCodeURL(state), http.StatusFound)
	})

	http.HandleFunc("/oauth2/callback", func(w http.ResponseWriter, r *http.Request) {
		//log.Printf("%s %s", r.Method, r.RequestURI)
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state did not match", http.StatusBadRequest)
			return
		}

		oauth2Token, err := config.Exchange(ctx, r.URL.Query().Get("code"))
		if err != nil {
			http.Error(w, "cannot get token", http.StatusBadRequest)
			return
		}
		if !oauth2Token.Valid() {
			http.Error(w, "token expired", http.StatusBadRequest)
			return
		}

		token := oauth2Token.Extra("access_token").(string)

		credsIAM.AccessToken = token
		credsIAM.RefreshToken = oauth2Token.Extra("refresh_token").(string)

		err = ioutil.WriteFile(".token", []byte(token), 0600)
		if err != nil {
			log.Println(fmt.Errorf("Could not save token file: %s", err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		//fmt.Println(token)

		//sts, err := credentials.NewSTSWebIdentity("https://131.154.97.121:9001/", getWebTokenExpiry)
		providers := []credentials.Provider{
			&IAMProvider{
				StsEndpoint: s.S3Endpoint,
				Token:       token,
				HTTPClient:  &s.Client.HTTPClient,
			},
		}

		sts := credentials.NewChainCredentials(providers)
		if err != nil {
			log.Println(fmt.Errorf("Could not set STS credentials: %s", err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		creds, err := sts.Get()
		if err != nil {
			log.Println(fmt.Errorf("Could not get STS credentials: %s", err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		//fmt.Println(creds)

		response := make(map[string]interface{})
		response["credentials"] = creds
		_, err = json.MarshalIndent(response, "", "\t")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write([]byte("VOLUME WILL BE MOUNTED IN FEW SECS, YOU CAN NOW CLOSE THIS TAB. \n"))
		//w.Write(c)

		sigint <- 1

	})

	address := fmt.Sprintf("localhost:3128")
	urlBrowse := fmt.Sprintf("http://%s/", address)
	log.Printf("listening on http://%s/", address)
	err := browser.OpenURL(urlBrowse)
	if err != nil {
		panic(err)
	}

	srv := &http.Server{Addr: address}

	idleConnsClosed := make(chan struct{})
	go func() {
		<-sigint

		// We received an interrupt signal, shut down.
		if err := srv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout:
			log.Printf("HTTP server Shutdown: %v", err)
		}
		close(idleConnsClosed)
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		// Error starting or closing listener:
		log.Fatalf("HTTP server ListenAndServe: %v", err)
	}

	<-idleConnsClosed

	confRClone := RCloneStruct{
		Address:  s.S3Endpoint,
		Instance: s.Instance,
	}

	tmpl, err := template.New("client").Parse(iamTmpl.RCloneTemplate)
	if err != nil {
		panic(err)
	}

	var b bytes.Buffer
	err = tmpl.Execute(&b, confRClone)
	if err != nil {
		panic(err)
	}

	rclone := b.String()

	err = ioutil.WriteFile(s.Client.ConfDir+"/"+"rclone.conf", []byte(rclone), 0600)
	if err != nil {
		panic(err)
	}

	MountVolume(s.Instance, s.RemotePath, s.LocalPath, s.Client.ConfDir)

	fmt.Printf("Volume mounted on %s", s.LocalPath)

	// // TODO: start routine to keep token valid!
	// cntxt := &daemon.Context{
	// 	PidFileName: "mount.pid",
	// 	PidFilePerm: 0644,
	// 	LogFileName: "mount.log",
	// 	LogFilePerm: 0640,
	// 	WorkDir:     "./",
	// }

	// d, err := cntxt.Reborn()
	// if err != nil {
	// 	return err
	// }
	// if d != nil {
	// 	return fmt.Errorf("Process exists")
	// }
	// defer cntxt.Release()

	// log.Print("- - - - - - - - - - - - - - -")
	// log.Print("daemon started")

	for {
		v := url.Values{}

		v.Set("client_id", clientResponse.ClientID)
		v.Set("client_secret", clientResponse.ClientSecret)
		v.Set("grant_type", "refresh_token")
		v.Set("refresh_token", credsIAM.RefreshToken)

		url, err := url.Parse(endpoint + "/token" + "?" + v.Encode())

		req := http.Request{
			Method: "POST",
			URL:    url,
		}

		// TODO: retrieve token with https POST with t.httpClient
		r, err := s.Client.HTTPClient.Do(&req)
		if err != nil {
			panic(err)
		}
		//fmt.Println(r.StatusCode, r.Status)

		var bodyJSON RefreshTokenStruct

		rbody, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}

		//fmt.Println(string(rbody))
		err = json.Unmarshal(rbody, &bodyJSON)
		if err != nil {
			panic(err)
		}

		// TODO:
		//encrToken := core.Encrypt([]byte(bodyJSON.AccessToken, passwd)

		err = ioutil.WriteFile(".token", []byte(bodyJSON.AccessToken), 0600)
		if err != nil {
			panic(err)
		}

		time.Sleep(10 * time.Minute)

	}
}
