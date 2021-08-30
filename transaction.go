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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"maunium.net/go/mautrix/appservice"
)

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

func readTransaction(w http.ResponseWriter, r *http.Request, into interface{}) *AppService {
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
		return nil
	}
	az, ok := cfg.byHSToken[token]
	if !ok {
		errUnknownToken.Write(w)
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(into)
	if err != nil {
		errBadJSON.Write(w)
		return nil
	}
	return az
}

func writeTransaction(w http.ResponseWriter, az *AppService, txnName, logContent string, payload interface{}) {
	conn := az.Conn()
	if conn != nil {
		az.writeLock.Lock()
		log.Printf("Sending %s to %s containing %s", txnName, az.ID, logContent)
		err := conn.WriteJSON(payload)
		az.writeLock.Unlock()
		if err != nil {
			log.Printf("Rejecting %s to %s: %v", txnName, az.ID, err)
			errSendFail.Write(w)
		} else {
			log.Printf("Sent %s to %s successfully", txnName, az.ID)
			appservice.WriteBlankOK(w)
		}
	} else {
		log.Printf("Rejecting %s to %s: websocket not connected", txnName, az.ID)
		errNotConnected.Write(w)
	}
}

func putTransaction(w http.ResponseWriter, r *http.Request) {
	var txn appservice.Transaction
	az := readTransaction(w, r, &txn)
	if az == nil {
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
	deviceListChanges := 0
	if txn.DeviceLists != nil {
		deviceListChanges = len(txn.DeviceLists.Changed)
	}
	vars := mux.Vars(r)
	txnID := vars["txnID"]
	logContent := fmt.Sprintf("%d events, %d ephemeral events, %d OTK counts and %d device list changes",
		len(txn.Events), len(txn.EphemeralEvents), len(txn.DeviceOTKCount), deviceListChanges)
	txnName := fmt.Sprintf("transaction %s", txnID)
	writeTransaction(w, az, txnName, logContent, &appservice.WebsocketTransaction{
		Status:      "ok",
		TxnID:       txnID,
		Transaction: txn,
	})
}

type SyncProxyError struct {
	appservice.Error
	TxnID string `json:"txn_id"`
}

func putSyncProxyError(w http.ResponseWriter, r *http.Request) {
	var txn appservice.Error
	az := readTransaction(w, r, &txn)
	if az == nil {
		return
	}
	vars := mux.Vars(r)
	txnID := vars["txnID"]
	logContent := string(txn.ErrorCode)
	txnName := fmt.Sprintf("syncproxy error %s", txnID)
	writeTransaction(w, az, txnName, logContent, &appservice.WebsocketRequest{
		Command: "syncproxy_error",
		Data: &SyncProxyError{
			Error: txn,
			TxnID: txnID,
		},
	})
}
