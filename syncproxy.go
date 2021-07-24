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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

var syncProxyClient = http.Client{Timeout: 2 * time.Minute}

// WSStartSyncRequest represents the content that bridges will send in start_sync requests.
type WSStartSyncRequest struct {
	AccessToken string      `json:"access_token"`
	UserID      id.UserID   `json:"user_id"`
	DeviceID    id.DeviceID `json:"device_id"`
}

// SPStartSyncRequest represents the content that mautrix-syncproxy expects in PUT /fi.mau.syncproxy requests.
type SPStartSyncRequest struct {
	AppserviceID   string      `json:"appservice_id"`
	UserID         id.UserID   `json:"user_id"`
	BotAccessToken string      `json:"bot_access_token"`
	DeviceID       id.DeviceID `json:"device_id"`
	HSToken        string      `json:"hs_token"`
	Address        string      `json:"address"`
	IsProxy        bool        `json:"is_proxy"`
}

// Sync proxy responses should be fairly small, so limit to 1 MiB
const maxBodySize = 1024 * 1024

func makeSyncProxyRequest(method, appserviceID string, data interface{}) error {
	var buf io.ReadWriter

	if data != nil {
		buf = &bytes.Buffer{}
		err := json.NewEncoder(buf).Encode(&data)
		if err != nil {
			return fmt.Errorf("failed to encode JSON for mautrix-syncproxy: %w", err)
		}
	}

	var err error
	var spURL string
	var req *http.Request
	var resp *http.Response
	var body []byte
	var respErr mautrix.RespError

	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	if spURL, err = cfg.SyncProxy.MakeURL(appserviceID); err != nil {
		return err
	} else if req, err = http.NewRequest(method, spURL, buf); err != nil {
		return fmt.Errorf("failed to prepare sync proxy %s request: %w", method, err)
	} else if req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.SyncProxy.SharedSecret)); len(cfg.SyncProxy.SharedSecret) == 0 {
		return fmt.Errorf("sync proxy shared secret not configured")
	} else if resp, err = syncProxyClient.Do(req); err != nil {
		return fmt.Errorf("failed to make sync proxy %s request: %w", method, err)
	} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	} else if body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodySize)); err != nil {
		return fmt.Errorf("sync proxy returned HTTP %d and no body", resp.StatusCode)
	} else if err = json.Unmarshal(body, &respErr); err != nil {
		return fmt.Errorf("sync proxy returned HTTP %d and non-JSON body (%w): %s", resp.StatusCode, err, body)
	}
	return respErr
}

func (az *AppService) startSyncProxy(rawReq json.RawMessage) error {
	var wsReq WSStartSyncRequest
	err := json.Unmarshal(rawReq, &wsReq)
	log.Println("Starting sync proxy for", az.ID, "/", wsReq.UserID, "/", wsReq.DeviceID)
	if err != nil {
		return fmt.Errorf("failed to parse request JSON: %w", err)
	}
	return makeSyncProxyRequest(http.MethodPut, az.ID, &SPStartSyncRequest{
		AppserviceID:   az.ID,
		UserID:         wsReq.UserID,
		BotAccessToken: wsReq.AccessToken,
		DeviceID:       wsReq.DeviceID,
		HSToken:        az.HS,
		Address:        cfg.SyncProxy.OwnURL,
		IsProxy:        true,
	})
}

func (az *AppService) stopSyncProxy() error {
	return makeSyncProxyRequest(http.MethodDelete, az.ID, nil)
}
