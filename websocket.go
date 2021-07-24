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
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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

const CloseConnReplaced = 4001

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
			err := az.stopSyncProxy()
			if err != nil {
				log.Println("Error requesting websocket stop after", az.ID, "disconnected:", err)
			}
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
		var msg appservice.WebsocketCommand
		err = ws.ReadJSON(&msg)
		if err != nil {
			log.Println("Error reading from websocket:", err)
			break
		}
		var resp interface{}
		switch msg.Command {
		case "start_sync":
			err = az.startSyncProxy(msg.Data)
			if err != nil {
				log.Println("Error forwarding", az.ID, "sync proxy start request:", err)
			}
		default:
			log.Printf("Unknown command %s in request #%d from websocket. Data: %s", msg.Command, msg.ReqID, msg.Data)
			err = fmt.Errorf("unknown command %s", msg.Command)
		}
		if msg.ReqID != 0 {
			respPayload := appservice.WebsocketRequest{
				ReqID:   msg.ReqID,
				Command: "response",
				Data:    resp,
			}
			if err != nil {
				respPayload.Command = "error"
				respPayload.Data = map[string]interface{}{
					"message": err.Error(),
				}
			}
			log.Printf("Sending response %+v", respPayload)
			err = ws.WriteJSON(&respPayload)
			if err != nil {
				log.Printf("Failed to send response to req #%d: %v", msg.ReqID, err)
			}
		}
	}
}
