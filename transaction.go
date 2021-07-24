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
		deviceListChanges := 0
		if txn.DeviceLists != nil {
			deviceListChanges = len(txn.DeviceLists.Changed)
		}
		az.writeLock.Lock()
		log.Printf("Sending transaction %s to %s containing %d events, %d ephemeral events, %d OTK counts and %d device list changes",
			txnID, az.ID, len(txn.Events), len(txn.EphemeralEvents), len(txn.DeviceOTKCount), deviceListChanges)
		err = conn.WriteJSON(&appservice.WebsocketTransaction{
			Status:      "ok",
			TxnID:       txnID,
			Transaction: txn,
		})
		az.writeLock.Unlock()
		if err != nil {
			log.Printf("Rejecting transaction %s to %s: %v", txnID, az.ID, err)
			errSendFail.Write(w)
		} else {
			log.Printf("Sent transaction %s to %s successfully", txnID, az.ID)
			appservice.WriteBlankOK(w)
		}
	} else {
		log.Printf("Rejecting transaction %s to %s: websocket not connected", txnID, az.ID)
		errNotConnected.Write(w)
	}
}
