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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	ebp "github.com/steelshot/eth-block-parser"
)

var (
	fDebug        bool
	fEndpoint     string
	fPollDuration time.Duration
)

var (
	mux    = http.NewServeMux()
	parser ebp.Parser
)

func init() {
	flag.BoolVar(&fDebug, "debug", false, "debug mode")
	flag.StringVar(&fEndpoint, "endpoint", "https://cloudflare-eth.com/", "eth endpoint")
	flag.DurationVar(&fPollDuration, "poll-duration", 10*time.Second, "poll duration")

	flag.Parse()

	if fDebug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	mux.HandleFunc("GET /currentBlock", handleGetCurrentBlock)
	mux.HandleFunc("PUT /subscribe/{address}", handleSubscribe)
	mux.HandleFunc("GET /transactions/{address}", handleGetTransactions)
}

func main() {
	var err error

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGKILL)
	defer stop()

	server := http.Server{
		Addr:    ":8123",
		Handler: mux,
		BaseContext: func(listener net.Listener) context.Context {
			return ctx
		}}

	slog.Info("Starting parser", "endpoint", fEndpoint)
	if parser, err = ebp.NewParser(ctx, fPollDuration, fEndpoint); err != nil {
		panic(err)
	}

	slog.Info("Starting api server", "address", ":8123")
	if err = server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func handleGetCurrentBlock(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("0x%X", parser.GetCurrentBlock())))
}

func handleSubscribe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	address := r.PathValue("address")

	switch subscribe, err := parser.Subscribe(address); {
	case err != nil:
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	case !subscribe:
		w.WriteHeader(http.StatusConflict)
	default:
		w.WriteHeader(http.StatusCreated)
	}
}

func handleGetTransactions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var (
		data    []byte
		address = r.PathValue("address")
	)

	if txs, err := parser.GetTransactions(address); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
	} else if len(txs) < 1 {
		w.WriteHeader(http.StatusNoContent)
	} else {
		if data, err = json.Marshal(txs); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
		} else if len(data) > 0 {
			w.WriteHeader(http.StatusOK)
			w.Write(data)
		}
	}
}
