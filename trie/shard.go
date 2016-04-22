// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package trie

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/tylertreat/BoomFilters"
)

const shardFilterPrecision = 0.001 // Precision to maintain for the scalable bloom filters
var shardPrefix = []byte("shard-") // Database prefix to use for trie node shards

// ShardCache is a database backed probabilistic data structure to predict
// which shard a trie node is in.
type ShardCache struct {
	db      Database                             // Database backing the shard presence filters
	shards  map[string]*boom.ScalableBloomFilter // Scalable bloom filters to test presence with
	updated map[string]struct{}                  // Set of shard filters that were set on
}

// NewShardCache creates a new shard cache baked by the specific database.
//
// Please ensure that no more that one cache is created for baking database to
// avoid database corruption and cache races!
func NewShardCache(db Database) *ShardCache {
	return &ShardCache{
		db:      db,
		shards:  make(map[string]*boom.ScalableBloomFilter),
		updated: make(map[string]struct{}),
	}
}

// Test checks whether there's a reasonable possibility that the specified key
// is in the given shard or not.
func (cache *ShardCache) Test(shard []byte, key []byte) bool {
	id := string(shard)

	// If the filter is already cached, test and return
	if filter, ok := cache.shards[id]; ok {
		return filter.Test(key)
	}
	// Otherwise load the filter from the database
	filter := boom.NewDefaultScalableBloomFilter(shardFilterPrecision)
	//filter.SetHash(new(shardHasher))

	blob, err := cache.db.Get(append(shardPrefix, shard...))
	if err == nil && len(blob) > 0 {
		if _, err := filter.ReadFrom(bytes.NewReader(blob)); err != nil {
			panic(err)
		}
	}
	// Cache for later, test and return
	cache.shards[id] = filter
	return filter.Test(key)
}

// Lookup iterates over all the shards in the database and retrieves all the
// matches that could contain the requested key.
func (cache *ShardCache) Lookup(key []byte) [][]byte {
	shards := make([][]byte, 0, 1) // Ideally one result

	// Iterate over all the shard filters and test for possible matches
	for index := uint64(0); ; index++ {
		// Generate the shard id from the index
		shard := make([]byte, 8)
		binary.BigEndian.PutUint64(shard, index)

		id := string(shard)

		// If the filter is already cached, test and continue to the next one
		if filter, ok := cache.shards[id]; ok {
			if filter.Test(key) {
				shards = append(shards, shard)
			}
			continue
		}
		// The shard is not yet loaded, retrieve and stop if non existent
		blob, err := cache.db.Get(append(shardPrefix, shard...))
		if err != nil || len(blob) == 0 {
			break
		}
		// Filter is indeed known, cache and test
		filter := boom.NewDefaultScalableBloomFilter(shardFilterPrecision)
		//filter.SetHash(new(shardHasher))

		if _, err := filter.ReadFrom(bytes.NewReader(blob)); err != nil {
			panic(err)
		}
		cache.shards[id] = filter
		if filter.Test(key) {
			shards = append(shards, shard)
		}
	}
	// Return any accumulated shards
	if len(shards) > 1 {
		fmt.Println("Wasteful lookup", len(shards))
	}
	return shards
}

// Set injects the current key into the shard cache.
func (cache *ShardCache) Set(shard []byte, key []byte) {
	// Test the key's presence (caches the filter) and short circuit if known
	if cache.Test(shard, key) {
		return
	}
	// Not present, but filter surely cached
	id := string(shard)

	filter := cache.shards[id]
	filter.Add(key)

	cache.updated[id] = struct{}{}
}

// Commit serializes all the modified shard filters into the database.
func (cache *ShardCache) Commit(db DatabaseWriter) error {
	// Iterate over all the dirty shard filters and serialize them
	/*for id, _ := range cache.updated {
		blob := new(bytes.Buffer)
		cache.shards[id].WriteTo(blob)

		if err := cache.db.Put(append(shardPrefix, []byte(id)...), blob.Bytes()); err != nil {
			return err
		}
	}*/
	// Clear the cache and return
	cache.updated = make(map[string]struct{})

	return nil
}

// shardHasher is a noop wrapper to convert node hashes to ones used by the
// scalable bloom filters.
type shardHasher struct {
	data []byte
}

func (h *shardHasher) Write(p []byte) (n int, err error) { h.data = p; return len(p), nil }
func (h *shardHasher) Sum(b []byte) []byte               { return append(b, h.data[:8]...) }
func (h *shardHasher) Reset()                            { h.data = nil }
func (h *shardHasher) Size() int                         { return 8 }
func (h *shardHasher) BlockSize() int                    { return 32 }
func (h *shardHasher) Sum64() uint64                     { return binary.BigEndian.Uint64(h.data[:8]) }
