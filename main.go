package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"

	"github.com/dciangot/sts-wire/pkg/core"
	iamTemplate "github.com/dciangot/sts-wire/pkg/template"
	"github.com/dciangot/sts-wire/pkg/validator"
	_ "github.com/go-bindata/go-bindata"
)

func main() {

	var (
		localMountPath string
	)

	inputReader := *bufio.NewReader(os.Stdin)
	scanner := core.GetInputWrapper{
		Scanner: inputReader,
	}

	instance := os.Args[1]
	if instance == "-h" {
		fmt.Println("IAM_SERVER=\"https://myIAM.com\" sts-wire <instance name> <s3 endpoint> <rclone remote path> <local mount point>")
		return
	}

	if len(os.Args) < 4 {
		fmt.Println("Invalid instance name, please type 'sts-wire -h'  for help")
		return
	}

	confDir := "." + instance

	_, err := os.Stat(confDir)
	if os.IsNotExist(err) {
		os.Mkdir(confDir, os.ModePerm)
	}

	s3Endpoint := os.Args[2]

	remote := os.Args[3]

	if validLocalPath, err := validator.LocalPath(os.Args[4]); validLocalPath {
		localMountPath = os.Args[4]
	} else {
		panic(err)
	}

	//fmt.Println(instance)

	// if instance == "" {
	// 	instance, err := scanner.GetInputString("Insert a name for the instance: ", "")
	// 	if err != nil {
	// 		panic(err)
	// 	} else if instance == "" {
	// 		panic(fmt.Errorf("Please insert a valid name."))
	// 	}
	// }

	clientConfig := core.IAMClientConfig{
		Host:       "localhost",
		Port:       3128,
		ClientName: "oidc-client",
	}

	// Create a CA certificate pool and add cert.pem to it
	//caCert, err := ioutil.ReadFile("MINIO.pem")
	//if err != nil {
	//	log.Fatal(err)
	//}
	//caCertPool := x509.NewCertPool()
	//caCertPool.AppendCertsFromPEM(caCert)

	// Create the TLS Config with the CA pool and enable Client certificate validation
	cfg := &tls.Config{
		//ClientCAs: caCertPool,
		InsecureSkipVerify: true,
	}
	//cfg.BuildNameToCertificate()

	tr := &http.Transport{
		TLSClientConfig: cfg,
	}

	httpClient := &http.Client{
		Transport: tr,
	}

	iamServer := ""
	if os.Getenv("IAM_SERVER") != "" {
		iamServer = os.Getenv("IAM_SERVER")
	}

	clientIAM := core.InitClientConfig{
		ConfDir:        confDir,
		ClientConfig:   clientConfig,
		Scanner:        scanner,
		HTTPClient:     *httpClient,
		IAMServer:      iamServer,
		ClientTemplate: iamTemplate.ClientTemplate,
		NoPWD:          false,
	}

	// Client registration
	endpoint, clientResponse, _, err := clientIAM.InitClient(instance)
	if err != nil {
		panic(err)
	}

	// TODO: use refresh_token
	if os.Getenv("REFRESH_TOKEN") != "" {
		clientResponse.ClientID = os.Getenv("IAM_CLIENT_ID")
		clientResponse.ClientSecret = os.Getenv("IAM_CLIENT_SECRET")
	}

	fmt.Println(clientResponse.Endpoint)

	server := core.Server{
		Client:     clientIAM,
		Instance:   instance,
		S3Endpoint: s3Endpoint,
		RemotePath: remote,
		LocalPath:  localMountPath,
		Endpoint:   endpoint,
		Response:   clientResponse,
	}

	err = server.Start()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Server started and volume mounted in %s", localMountPath)
	fmt.Printf("To unmount you can see you PID in mount.pid file and kill it.")

}
