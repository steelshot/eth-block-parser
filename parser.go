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

package eth_block_parser

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/steelshot/eth-block-parser/rpc"
)

const (
	MaxTx = 1000
)

type (
	Parser interface {
		// GetCurrentBlock last parsed block
		GetCurrentBlock() uint64
		// Subscribe add address to observer
		Subscribe(address string) (bool, error)
		// GetTransactions list of inbound or outbound transactions for an address
		GetTransactions(address string) ([]rpc.Transaction, error)
	}
)

type (
	parser struct {
		hashChan chan string
		txChan   chan rpc.Transaction

		endpoint  *url.URL
		block     atomic.Uint64
		addresses sync.Map
	}
)

func NewParser(ctx context.Context, pollDuration time.Duration, endpoint string) (_ Parser, err error) {
	p := &parser{
		hashChan: make(chan string, math.MaxUint16),
		txChan:   make(chan rpc.Transaction, math.MaxUint16),
	}

	if p.endpoint, err = url.Parse(endpoint); err != nil {
		return nil, fmt.Errorf("invalid endpoint: %s", err)
	}

	go p.blockPoll(ctx, pollDuration)
	go p.transactionPoll(ctx)
	go p.assignTxsToSubs(ctx)

	return p, nil
}

func (p *parser) GetCurrentBlock() uint64 {
	slog.Info("parser::GetCurrentBlock()")

	return p.block.Load()
}

func (p *parser) Subscribe(address string) (success bool, _ error) {
	slog.Info("parser::Subscribe()", "address", address)

	if err := isValidAddress(address); err != nil {
		return false, err
	}

	// Normalize addresses to lowercase
	address = strings.ToLower(address)

	_, loaded := p.addresses.LoadOrStore(address, []rpc.Transaction{})

	if !loaded {
		slog.Info("Subscribed", "address", address)
	}

	return !loaded, nil
}

func (p *parser) GetTransactions(address string) (_ []rpc.Transaction, err error) {
	slog.Info("parser::GetTransactions()", "address", address)

	if err = isValidAddress(address); err != nil {
		return nil, err
	}

	// Normalize addresses to lowercase
	address = strings.ToLower(address)

	if value, ok := p.addresses.Load(address); !ok {
		return nil, fmt.Errorf("address %s not subscribed", address)
	} else {
		return value.([]rpc.Transaction), nil
	}
}

func (p *parser) blockPoll(ctx context.Context, pollDuration time.Duration) {
	wait := time.After(0)

	slog.Debug("parser::blockPoll - Start")
BlockPoll:
	for {
		select {
		case <-ctx.Done():
			break BlockPoll
		case <-wait:
			slog.Info("Getting latest block")
			wait = time.After(pollDuration)

			var (
				err   error
				block rpc.Block
			)

			slog.Debug("parser::blockPoll - parser::getLatestBlock()")
			if block, err = p.getLatestBlock(); err != nil {
				slog.Warn(fmt.Errorf("failed getting latest block: %w", err).Error())

				break
			} else {
				// If still working with the same block, ignore transactions till new one
				if p.block.Swap(block.Number) == block.Number {
					break
				}
			}

			slog.Info("Got latest block",
				"number", fmt.Sprintf("0x%X", block.Number),
				"txCount", len(block.TxHashes))

			// Artificial cap, to not hit rate limits
			if len(block.TxHashes) > MaxTx {
				block.TxHashes = block.TxHashes[:MaxTx]
			}

			for _, hash := range block.TxHashes {
				p.hashChan <- hash
			}
		}
	}

	slog.Debug("parser::blockPoll - End")
}

func (p *parser) transactionPoll(ctx context.Context) {
	slog.Debug("parser::transactionPoll - Start")
TxPoll:
	for {
		select {
		case <-ctx.Done():
			break TxPoll
		case hash := <-p.hashChan:
			slog.Debug("parser::transactionPoll - parser::getTransactionByHash()")
			if tx, err := p.getTransactionByHash(hash); err != nil {
				slog.Warn(fmt.Errorf("failed to get transaction %s: %w", hash, err).Error())
			} else {
				p.txChan <- tx
			}
		}
	}

	slog.Debug("parser::transactionPoll - End")
}

func (p *parser) assignTxsToSubs(ctx context.Context) {
	slog.Debug("parser::assignTxsToSubs - Start")
TxToSubs:
	for {
		select {
		case <-ctx.Done():
			break TxToSubs
		case tx := <-p.txChan:
			if value, ok := p.addresses.Load(tx.To); ok {
				slog.Info(fmt.Sprintf("found tx for recv sub %s", tx.To))

				p.addresses.Store(tx.To, append(value.([]rpc.Transaction), tx))
			}

			if value, ok := p.addresses.Load(tx.From); ok {
				slog.Info(fmt.Sprintf("found tx for send sub %s", tx.To))

				p.addresses.Store(tx.From, append(value.([]rpc.Transaction), tx))
			}
		}
	}

	slog.Debug("parser::assignTxsToSubs - End")
}

func (p *parser) getLatestBlock() (block rpc.Block, err error) {
	var data []byte

	slog.Debug("parser::getLatestBlock - ebp::makeRequest()")
	if data, err = makeRequest(http.MethodPost, p.endpoint.String(), "", "eth_getBlockByNumber", "latest", false); err != nil {
		return block, fmt.Errorf("failed to execute eth_getBlockByNumber request: %w", err)
	}

	// Temp struct to hold the latest block
	var (
		response struct {
			Result struct {
				Number string   `json:"number"`
				Hashes []string `json:"transactions"`
			} `json:"result"`
		}

		number uint64
	)

	slog.Debug("parser::getLatestBlock - json::Unmarshal()")
	if err = json.Unmarshal(data, &response); err != nil {
		return block, fmt.Errorf("eth_getBlockByNumber returned invalid data: %w", err)
	}

	newBlock := response.Result

	// Since the eth JSON rpc always returns numbers as hex with a suffix 0x, we can let ParseInt assume the base: 0x = 16
	// Otherwise we can use strconv.ParseInt(strings.TrimPrefix(result.Result, "0x"), 16, 64)
	slog.Debug("parser::getLatestBlock - strconv::ParseUint()")
	if number, err = strconv.ParseUint(newBlock.Number, 0, 64); err != nil {
		return block, fmt.Errorf("eth_blockNumber returned invalid data: %w", err)
	}

	block = rpc.Block{
		Number:   number,
		TxHashes: newBlock.Hashes,
	}

	return block, nil
}

func (p *parser) getTransactionByHash(hash string) (tx rpc.Transaction, err error) {
	var data []byte

	slog.Debug("parser::getTransactionByHash - makeRequest()")
	if data, err = makeRequest(http.MethodPost, p.endpoint.String(), "", "eth_getTransactionByHash", hash); err != nil {
		return tx, fmt.Errorf("failed to execute eth_getTransactionByHash request: %w", err)
	}

	// Temp struct to hold the latest block
	var (
		response struct {
			Tx rpc.Transaction `json:"result"`
		}
	)

	slog.Debug("parser::getTransactionByHash - json::Unmarshal()")
	if err = json.Unmarshal(data, &response); err != nil {
		return tx, fmt.Errorf("eth_getTransactionByHash returned invalid data: %w", err)
	}

	return response.Tx, nil
}

func makeRequest(method, url, id, ethMethod string, params ...interface{}) (data []byte, err error) {
	var (
		req  *http.Request
		resp *http.Response
	)

	slog.Debug("ebp::makeRequest - rpc::NewRequest()")
	if req, err = rpc.NewRequest(method, url, id, ethMethod, params...); err != nil {
		return nil, fmt.Errorf("failed to create %s request: %w", ethMethod, err)
	}

	slog.Debug("ebp::makeRequest - http::Do()")
	if resp, err = http.DefaultClient.Do(req); err != nil {
		return nil, fmt.Errorf("failed to execute %s request: %w", ethMethod, err)
	}
	defer resp.Body.Close()

	// Not documented, assuming only 200 is valid
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to execute %s request: %s", ethMethod, resp.Status)
	}

	slog.Debug("ebp::makeRequest - io::ReadAll()")
	if data, err = io.ReadAll(resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read %s response body: %w", ethMethod, err)
	}

	return
}

func isValidAddress(address string) error {
	if l := len(address); l != 42 {
		return fmt.Errorf("invalid address length: %d, expected 42", l)
	}

	if _, err := hex.DecodeString(address[2:]); err != nil {
		return fmt.Errorf("invalid address format: %w", err)
	}

	return nil
}
