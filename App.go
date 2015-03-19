package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/ninjasphere/go-ninja/api"
	"github.com/ninjasphere/go-ninja/config"
	"github.com/ninjasphere/go-ninja/support"
	"github.com/ninjasphere/go-uber"
	"github.com/ninjasphere/sphere-go-led-controller/remote"
)

var info = ninja.LoadModuleInfo("./package.json")

var sandbox = config.MustBool("uber.sandbox")

var uberConfig UberConfig

var client *uber.Client
var user *uber.User

type UberConfig struct {
	ClientID    string `json:"clientId"`
	ServerToken string `json:"serverToken"`
	Secret      string `json:"secret"`
}

func init() {
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Failed to read config.json: %s", err)
	}

	err = json.Unmarshal(data, &uberConfig)
	if err != nil {
		log.Fatalf("Failed to parse config.json: %s", err)
	}

	if uberConfig.ClientID == "" || uberConfig.ServerToken == "" || uberConfig.Secret == "" {
		log.Fatalf("You must provide the uber config in config.json")
	}

	client = uber.NewClient(uberConfig.ServerToken)

	if sandbox {
		uber.UberAPIHost = "https://sandbox-api.uber.com/" + uber.Version
	}

	client.SetAuth(uberConfig.ClientID, uberConfig.Secret, "http://localhost:7635")
}

type RuntimeConfig struct {
}

type App struct {
	support.AppSupport
	led *remote.Matrix
}

func (a *App) Start(cfg *RuntimeConfig) error {

	access, err := loadUserToken()

	if err != nil {
		log.Infof("No user token. Creating a new one.")
		err = client.AutOAuth("profile")

		if err != nil {
			log.Fatalf("Could not create user token: %s", err)
		}

		err = saveUserToken()
		if err != nil {
			log.Fatalf("Could not save user token to file token.json: %s", err)
		}
	} else {
		client.Access = access
	}

	user, err = client.GetUserProfile()

	if err != nil {

		if strings.Contains(err.Error(), "unauthorized") {

			err = client.RefreshAccessToken()

			if err == nil {

				user, err = client.GetUserProfile()

				if err != nil {
					log.Fatalf("Unauthorised. Refreshed but still couldn't get profile. %s", err)
				}

			} else {
				deleteUserToken()
				log.Fatalf("Unauthorised. Couldn't refresh access token. Deleted token. %s", err)
			}
		} else {
			log.Fatalf("Failed to get user profile: %s", err)
		}
	}

	spew.Dump("Got user profile", user)

	pane := NewUberPane(a.Conn)

	// Connect to the led controller remote pane interface
	tcpAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		println("ResolveTCPAddr failed:", err.Error())
		os.Exit(1)
	}

	log.Infof("Connecting to led controller")
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		println("Dial failed:", err.Error())
		os.Exit(1)
	}

	log.Infof("Connected.")

	// Export our pane over this interface
	a.led = remote.NewMatrix(pane, conn)

	return nil
}

func loadUserToken() (*uber.Access, error) {
	b, err := ioutil.ReadFile("token.json")

	if err != nil {
		return nil, err
	}

	var access uber.Access
	err = json.Unmarshal(b, &access)
	return &access, err
}

func saveUserToken() error {
	b, err := json.Marshal(client.Access)
	if err != nil {
		return err
	}
	return ioutil.WriteFile("token.json", b, 0644)
}

func deleteUserToken() error {
	return os.Remove("token.json")
}

// Stop the security light app.
func (a *App) Stop() error {
	a.led.Close()
	a.led = nil
	return nil
}
