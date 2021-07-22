// mautrix-wsproxy - A simple HTTP push -> websocket proxy for Matrix appservices.
// Copyright (C) 2021 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v2"

	"maunium.net/go/mautrix/appservice"
)

type AppService struct {
	ID string `yaml:"id"`
	AS string `yaml:"as"`
	HS string `yaml:"hs"`

	conn     *websocket.Conn `yaml:"-"`
	connLock sync.Mutex      `yaml:"-"`
}

func (az *AppService) Conn() *websocket.Conn {
	return az.conn
}

type Config struct {
	ListenAddress string        `yaml:"listen_address"`
	AppServices   []*AppService `yaml:"appservices"`

	SyncProxy struct {
		URL          string `yaml:"url"`
		SharedSecret string `yaml:"shared_secret"`
	} `yaml:"sync_proxy"`

	byASToken map[string]*AppService `yaml:"-"`
	byHSToken map[string]*AppService `yaml:"-"`
}

const CloseConnReplaced = 4001

var cfg Config

var (
	errMissingToken = appservice.Error{
		HTTPStatus: http.StatusForbidden,
		ErrorCode:  "M_MISSING_TOKEN",
		Message:    "Missing authorization header",
	}
	errUnknownToken = appservice.Error{
		HTTPStatus: http.StatusForbidden,
		ErrorCode:  "M_UNKNOWN_TOKEN",
		Message:    "Unknown authorization token",
	}
	errBadJSON = appservice.Error{
		HTTPStatus: http.StatusBadRequest,
		ErrorCode:  "M_BAD_JSON",
		Message:    "Failed to decode request JSON",
	}
	errSendFail = appservice.Error{
		HTTPStatus: http.StatusBadGateway,
		ErrorCode:  "FI.MAU.WS_SEND_FAIL",
		Message:    "Failed to send data through websocket",
	}
	errNotConnected = appservice.Error{
		HTTPStatus: http.StatusBadGateway,
		ErrorCode:  "FI.MAU.WS_NOT_CONNECTED",
		Message:    "Endpoint is not connected to websocket",
	}
)

func putTransaction(w http.ResponseWriter, r *http.Request) {
	var token string
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		token = r.URL.Query().Get("access_token")
	} else {
		token = authHeader[len("Bearer "):]
	}
	w.Header().Add("Content-Type", "application/json")
	if len(token) == 0 {
		errMissingToken.Write(w)
		return
	}
	az, ok := cfg.byHSToken[token]
	if !ok {
		errUnknownToken.Write(w)
		return
	}
	var txn appservice.Transaction
	err := json.NewDecoder(r.Body).Decode(&txn)
	if err != nil {
		errBadJSON.Write(w)
		return
	}
	if txn.EphemeralEvents == nil && txn.MSC2409EphemeralEvents != nil {
		txn.EphemeralEvents = txn.MSC2409EphemeralEvents
	}
	if txn.DeviceLists == nil && txn.MSC3202DeviceLists != nil {
		txn.DeviceLists = txn.MSC3202DeviceLists
	}
	if txn.DeviceOTKCount == nil && txn.MSC3202DeviceOTKCount != nil {
		txn.DeviceOTKCount = txn.MSC3202DeviceOTKCount
	}
	vars := mux.Vars(r)
	txnID := vars["txnID"]
	conn := az.Conn()
	if conn != nil {
		err = conn.WriteJSON(appservice.WebsocketTransaction{
			Status:      "ok",
			TxnID:       txnID,
			Transaction: txn,
		})
		if err != nil {
			log.Printf("Rejecting transaction %s to %s: %v", txnID, az.ID, err)
			errSendFail.Write(w)
		} else {
			log.Printf("Sent transaction %s to %s containing %d events and %d ephemeral events",
				txnID, az.ID, len(txn.Events), len(txn.EphemeralEvents))
			appservice.WriteBlankOK(w)
		}
	} else {
		log.Printf("Rejecting transaction %s to %s: websocket not connected", txnID, az.ID)
		errNotConnected.Write(w)
	}
}

var upgrader = websocket.Upgrader{}

func syncWebsocket(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		errMissingToken.Write(w)
		return
	}
	az, ok := cfg.byASToken[authHeader[len("Bearer "):]]
	if !ok {
		errUnknownToken.Write(w)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade websocket request:", err)
		return
	}
	log.Println(az.ID, "connected to websocket")
	defer func() {
		log.Println(az.ID, "disconnected from websocket")
		az.connLock.Lock()
		if az.conn == ws {
			az.conn = nil
		}
		az.connLock.Unlock()
		_ = ws.Close()
	}()
	err = ws.WriteMessage(websocket.TextMessage, []byte(`{"status": "connected"}`))
	if err != nil {
		log.Printf("Failed to write welcome status message to %s: %v", az.ID, err)
	}
	az.connLock.Lock()
	if az.conn != nil {
		go func(oldConn *websocket.Conn) {
			msg := websocket.FormatCloseMessage(CloseConnReplaced, `{"command": "disconnect", "status": "conn_replaced"}`)
			_ = oldConn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(3*time.Second))
			_ = oldConn.Close()
		}(az.conn)
	}
	az.conn = ws
	az.connLock.Unlock()
	for {
		_, _, err = ws.ReadMessage()
		if err != nil {
			log.Println("Error reading from websocket:", err)
			break
		}
	}
}

var configPath = flag.String("config", "config.yaml", "path to config file")

func main() {
	flag.Parse()
	if *configPath == "env" {
		cfg.ListenAddress = os.Getenv("LISTEN_ADDRESS")
		cfg.AppServices = []*AppService{{
			ID: os.Getenv("APPSERVICE_ID"),
			AS: os.Getenv("AS_TOKEN"),
			HS: os.Getenv("HS_TOKEN"),
		}}
		cfg.SyncProxy.URL = os.Getenv("SYNC_PROXY_URL")
		cfg.SyncProxy.SharedSecret = os.Getenv("SYNC_PROXY_SHARED_SECRET")
		if len(cfg.ListenAddress) == 0 {
			log.Fatalln("LISTEN_ADDRESS environment variable is not set")
		} else if len(cfg.AppServices[0].ID) == 0 {
			log.Fatalln("APPSERVICE_ID environment variable is not set")
		} else if len(cfg.AppServices[0].AS) == 0 {
			log.Fatalln("AS_TOKEN environment variable is not set")
		} else if len(cfg.AppServices[0].HS) == 0 {
			log.Fatalln("HS_TOKEN environment variable is not set")
		}
		log.Printf("Found one appservice from environment variables")
	} else {
		file, err := os.Open(*configPath)
		if err != nil {
			log.Fatalln("Failed to open config:", err)
		}
		err = yaml.NewDecoder(file).Decode(&cfg)
		if err != nil {
			log.Fatalln("Failed to read config:", err)
		} else if len(cfg.AppServices) == 0 {
			log.Fatalln("No appservices configured")
		} else if len(cfg.ListenAddress) == 0 {
			log.Fatalln("Listen address not configured")
		}
		appservices := "appservices"
		if len(cfg.AppServices) == 1 {
			appservices = "appservice"
		}
		log.Println("Found", len(cfg.AppServices), appservices, "in", *configPath)
	}
	cfg.byHSToken = make(map[string]*AppService)
	cfg.byASToken = make(map[string]*AppService)
	for i, az := range cfg.AppServices {
		if len(az.ID) == 0 {
			log.Fatalf("Appservice #%d doesn't have an ID", i+1)
		} else if len(az.AS) == 0 {
			log.Fatalf("Appservice %s doesn't have the AS token set", az.ID)
		} else if len(az.AS) == 0 {
			log.Fatalf("Appservice %s doesn't have the HS token set", az.ID)
		}
		cfg.byASToken[az.AS] = az
		cfg.byHSToken[az.HS] = az
	}
	router := mux.NewRouter()
	router.HandleFunc("/transactions/{txnID}", putTransaction).Methods(http.MethodPut)
	router.HandleFunc("/_matrix/app/v1/transactions/{txnID}", putTransaction).Methods(http.MethodPut)
	router.HandleFunc("/_matrix/client/unstable/fi.mau.as_sync", syncWebsocket).Methods(http.MethodGet)
	server := &http.Server{
		Addr:    cfg.ListenAddress,
		Handler: router,
	}
	go func() {
		log.Println("Starting to listen on", cfg.ListenAddress)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalln("Error in listener:", err)
		}
	}()

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	for _, az := range cfg.AppServices {
		go func(oldConn *websocket.Conn) {
			if oldConn == nil {
				return
			}
			msg := websocket.FormatCloseMessage(websocket.CloseGoingAway, `{"command": "disconnect", "status": "server_shutting_down"}`)
			_ = oldConn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(3*time.Second))
			_ = oldConn.Close()
		}(az.conn)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := server.Shutdown(ctx)
	if err != nil {
		log.Println("Failed to close server:", err)
	}
}
