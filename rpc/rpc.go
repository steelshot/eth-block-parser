/*
 * Copyright (C) 2024 Džiugas Eiva GPL-3.0-only
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation version 3 of the License
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package rpc

import (
	"bytes"
	"encoding/json"
	"net/http"
)

type (
	Request struct {
		Version string        `json:"jsonrpc"`
		Method  string        `json:"method"`
		Params  []interface{} `json:"params"`
		Id      string        `json:"id"`
	}

	Block struct {
		Number   uint64   `json:"number"`
		TxHashes []string `json:"transactions"`
	}

	Transaction struct {
		Hash  string `json:"hash"`
		Block string `json:"blockNumber"`
		To    string `json:"to"`
		From  string `json:"from"`
	}
)

func NewRequest(method, url, id, ethMethod string, params ...interface{}) (_ *http.Request, err error) {
	var (
		data []byte
		r    = Request{
			Version: "2.0",
			Method:  ethMethod,
			Params:  params,
			Id:      id,
		}
	)

	if data, err = json.Marshal(r); err != nil {
		return nil, err
	}

	return http.NewRequest(method, url, bytes.NewBuffer(data))
}
