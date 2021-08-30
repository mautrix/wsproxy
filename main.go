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
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func main() {
	loadConfig()
	router := mux.NewRouter()
	router.HandleFunc("/transactions/{txnID}", putTransaction).Methods(http.MethodPut)
	router.HandleFunc("/_matrix/app/v1/transactions/{txnID}", putTransaction).Methods(http.MethodPut)
	router.HandleFunc("/_matrix/app/unstable/fi.mau.syncproxy/error/{txnID}", putSyncProxyError).Methods(http.MethodPut)
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
