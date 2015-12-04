// Copyright 2015 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package downloader

import (
	rpc "github.com/ethereum/go-ethereum/rpc/v2"
)

type DownloaderService struct {
	d *Downloader
}

func NewDownloaderService(d *Downloader) *DownloaderService {
	return &DownloaderService{d}
}

type Progress struct {
	Origin  uint64 `json:"startingBlock"`
	Current uint64 `json:"currentBlock"`
	Height  uint64 `json:"highestBlock"`
}

type SyncingResult struct {
	Syncing bool     `json:"syncing"`
	Status  Progress `json:"status"`
}

func (s *DownloaderService) Syncing() (rpc.Subscription, error) {
	sub := s.d.mux.Subscribe(StartEvent{}, DoneEvent{}, FailedEvent{})

	output := func(event interface{}) interface{} {
		switch event.(type) {
		case StartEvent:
			result := &SyncingResult{Syncing: true}
			result.Status.Origin, result.Status.Current, result.Status.Height = s.d.Progress()
			return result
		case DoneEvent, FailedEvent:
			return false
		}
		return nil
	}

	return rpc.NewSubscriptionWithOutputFormat(sub, output), nil
}
