package eth

import (
	"fmt"
	"math/big"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
)

// Tests that hashes can be retrieved from a remote chain by hashes in reverse
// order.
func TestGetBlockHashes60(t *testing.T) { testGetBlockHashes(t, 60) }
func TestGetBlockHashes61(t *testing.T) { testGetBlockHashes(t, 61) }

func testGetBlockHashes(t *testing.T, protocol int) {
	pm := newTestProtocolManager(downloader.MaxHashFetch+15, nil, nil)
	peer, _ := newTestPeer("peer", protocol, pm, true)
	defer peer.close()

	// Create a batch of tests for various scenarios
	limit := downloader.MaxHashFetch
	tests := []struct {
		origin common.Hash
		number int
		result int
	}{
		{common.Hash{}, 1, 0},                                 // Make sure non existent hashes don't return results
		{pm.chainman.Genesis().Hash(), 1, 0},                  // There are no hashes to retrieve up from the genesis
		{pm.chainman.GetBlockByNumber(5).Hash(), 5, 5},        // All the hashes including the genesis requested
		{pm.chainman.GetBlockByNumber(5).Hash(), 10, 5},       // More hashes than available till the genesis requested
		{pm.chainman.GetBlockByNumber(100).Hash(), 10, 10},    // All hashes available from the middle of the chain
		{pm.chainman.CurrentBlock().Hash(), 10, 10},           // All hashes available from the head of the chain
		{pm.chainman.CurrentBlock().Hash(), limit, limit},     // Request the maximum allowed hash count
		{pm.chainman.CurrentBlock().Hash(), limit + 1, limit}, // Request more than the maximum allowed hash count
	}
	// Run each of the tests and verify the results against the chain
	for i, tt := range tests {
		// Assemble the hash response we would like to receive
		resp := make([]common.Hash, tt.result)
		if len(resp) > 0 {
			from := pm.chainman.GetBlock(tt.origin).NumberU64() - 1
			for j := 0; j < len(resp); j++ {
				resp[j] = pm.chainman.GetBlockByNumber(uint64(int(from) - j)).Hash()
			}
		}
		// Send the hash request and verify the response
		p2p.Send(peer.app, 0x03, getBlockHashesData{tt.origin, uint64(tt.number)})
		if err := p2p.ExpectMsg(peer.app, 0x04, resp); err != nil {
			t.Errorf("test %d: block hashes mismatch: %v", i, err)
		}
	}
}

// Tests that hashes can be retrieved from a remote chain by numbers in forward
// order.
func TestGetBlockHashesFromNumber60(t *testing.T) { testGetBlockHashesFromNumber(t, 60) }
func TestGetBlockHashesFromNumber61(t *testing.T) { testGetBlockHashesFromNumber(t, 61) }

func testGetBlockHashesFromNumber(t *testing.T, protocol int) {
	pm := newTestProtocolManager(downloader.MaxHashFetch+15, nil, nil)
	peer, _ := newTestPeer("peer", protocol, pm, true)
	defer peer.close()

	// Create a batch of tests for various scenarios
	limit := downloader.MaxHashFetch
	tests := []struct {
		origin uint64
		number int
		result int
	}{
		{pm.chainman.CurrentBlock().NumberU64() + 1, 1, 0},     // Out of bounds requests should return empty
		{pm.chainman.CurrentBlock().NumberU64(), 1, 1},         // Make sure the head hash can be retrieved
		{pm.chainman.CurrentBlock().NumberU64() - 4, 5, 5},     // All hashes, including the head hash requested
		{pm.chainman.CurrentBlock().NumberU64() - 4, 10, 5},    // More hashes requested than available till the head
		{pm.chainman.CurrentBlock().NumberU64() - 100, 10, 10}, // All hashes available from the middle of the chain
		{0, 10, 10},           // All hashes available from the root of the chain
		{0, limit, limit},     // Request the maximum allowed hash count
		{0, limit + 1, limit}, // Request more than the maximum allowed hash count
		{0, 1, 1},             // Make sure the genesis hash can be retrieved
	}
	// Run each of the tests and verify the results against the chain
	for i, tt := range tests {
		// Assemble the hash response we would like to receive
		resp := make([]common.Hash, tt.result)
		for j := 0; j < len(resp); j++ {
			resp[j] = pm.chainman.GetBlockByNumber(tt.origin + uint64(j)).Hash()
		}
		// Send the hash request and verify the response
		p2p.Send(peer.app, 0x08, getBlockHashesFromNumberData{tt.origin, uint64(tt.number)})
		if err := p2p.ExpectMsg(peer.app, 0x04, resp); err != nil {
			t.Errorf("test %d: block hashes mismatch: %v", i, err)
		}
	}
}

// Tests that blocks can be retrieved from a remote chain based on their hashes.
func TestGetBlocks60(t *testing.T) { testGetBlocks(t, 60) }
func TestGetBlocks61(t *testing.T) { testGetBlocks(t, 61) }

func testGetBlocks(t *testing.T, protocol int) {
	pm := newTestProtocolManager(downloader.MaxHashFetch+15, nil, nil)
	peer, _ := newTestPeer("peer", protocol, pm, true)
	defer peer.close()

	// Create a batch of tests for various scenarios
	limit := downloader.MaxBlockFetch
	tests := []struct {
		random    int           // Number of blocks to fetch randomly from the chain
		explicit  []common.Hash // Explicitly requested blocks
		available []bool        // Availability of explicitly requested blocks
		expected  int           // Total number of existing blocks to expect
	}{
		{1, nil, nil, 1},                                                       // A single random block should be retrievable
		{10, nil, nil, 10},                                                     // Multiple random blocks should be retrievable
		{limit, nil, nil, limit},                                               // The maximum possible blocks should be retrievable
		{limit + 1, nil, nil, limit},                                           // No more that the possible block count should be returned
		{0, []common.Hash{pm.chainman.Genesis().Hash()}, []bool{true}, 1},      // The genesis block should be retrievable
		{0, []common.Hash{pm.chainman.CurrentBlock().Hash()}, []bool{true}, 1}, // The chains head block should be retrievable
		{0, []common.Hash{common.Hash{}}, []bool{false}, 0},                    // A non existent block should not be returned

		// Existing and non-existing blocks interleaved should not cause problems
		{0, []common.Hash{
			common.Hash{},
			pm.chainman.GetBlockByNumber(1).Hash(),
			common.Hash{},
			pm.chainman.GetBlockByNumber(10).Hash(),
			common.Hash{},
			pm.chainman.GetBlockByNumber(100).Hash(),
			common.Hash{},
		}, []bool{false, true, false, true, false, true, false}, 3},
	}
	// Run each of the tests and verify the results against the chain
	for i, tt := range tests {
		// Collect the hashes to request, and the response to expect
		hashes, seen := []common.Hash{}, make(map[int64]bool)
		blocks := []*types.Block{}

		for j := 0; j < tt.random; j++ {
			for {
				num := rand.Int63n(int64(pm.chainman.CurrentBlock().NumberU64()))
				if !seen[num] {
					seen[num] = true

					block := pm.chainman.GetBlockByNumber(uint64(num))
					hashes = append(hashes, block.Hash())
					if len(blocks) < tt.expected {
						blocks = append(blocks, block)
					}
					break
				}
			}
		}
		for j, hash := range tt.explicit {
			hashes = append(hashes, hash)
			if tt.available[j] && len(blocks) < tt.expected {
				blocks = append(blocks, pm.chainman.GetBlock(hash))
			}
		}
		// Send the hash request and verify the response
		p2p.Send(peer.app, 0x05, hashes)
		if err := p2p.ExpectMsg(peer.app, 0x06, blocks); err != nil {
			t.Errorf("test %d: blocks mismatch: %v", i, err)
		}
	}
}

// Tests that block headers can be retrieved from a remote chain based on their hashes.
func TestGetBlockHeaders62(t *testing.T) {
	pm := newTestProtocolManager(downloader.MaxHashFetch+15, nil, nil)
	peer, _ := newTestPeer("peer", 62, pm, true)
	defer peer.close()

	// Create a batch of tests for various scenarios
	limit := downloader.MaxHeaderFetch
	tests := []struct {
		random    int           // Number of blocks to fetch randomly from the chain
		explicit  []common.Hash // Explicitly requested blocks
		available []bool        // Availability of explicitly requested blocks
		expected  int           // Total number of existing blocks to expect
	}{
		{1, nil, nil, 1},                                                       // A single random block should be retrievable
		{10, nil, nil, 10},                                                     // Multiple random blocks should be retrievable
		{limit, nil, nil, limit},                                               // The maximum possible blocks should be retrievable
		{limit + 1, nil, nil, limit},                                           // No more that the possible block count should be returned
		{0, []common.Hash{pm.chainman.Genesis().Hash()}, []bool{true}, 1},      // The genesis block should be retrievable
		{0, []common.Hash{pm.chainman.CurrentBlock().Hash()}, []bool{true}, 1}, // The chains head block should be retrievable
		{0, []common.Hash{common.Hash{}}, []bool{false}, 0},                    // A non existent block should not be returned

		// Existing and non-existing blocks interleaved should not cause problems
		{0, []common.Hash{
			common.Hash{},
			pm.chainman.GetBlockByNumber(1).Hash(),
			common.Hash{},
			pm.chainman.GetBlockByNumber(10).Hash(),
			common.Hash{},
			pm.chainman.GetBlockByNumber(100).Hash(),
			common.Hash{},
		}, []bool{false, true, false, true, false, true, false}, 3},
	}
	// Run each of the tests and verify the results against the chain
	for i, tt := range tests {
		// Collect the hashes to request, and the response to expect
		hashes, seen := []common.Hash{}, make(map[int64]bool)
		headers := []*types.Header{}

		for j := 0; j < tt.random; j++ {
			for {
				num := rand.Int63n(int64(pm.chainman.CurrentBlock().NumberU64()))
				if !seen[num] {
					seen[num] = true

					block := pm.chainman.GetBlockByNumber(uint64(num))
					hashes = append(hashes, block.Hash())
					if len(headers) < tt.expected {
						headers = append(headers, block.Header())
					}
					break
				}
			}
		}
		for j, hash := range tt.explicit {
			hashes = append(hashes, hash)
			if tt.available[j] && len(headers) < tt.expected {
				headers = append(headers, pm.chainman.GetBlock(hash).Header())
			}
		}
		// Send the hash request and verify the response
		p2p.Send(peer.app, 0x03, hashes)
		if err := p2p.ExpectMsg(peer.app, 0x04, headers); err != nil {
			t.Errorf("test %d: headers mismatch: %v", i, err)
		}
	}
}

// Tests that the node state database can be retrieved based on hashes.
func TestGetNodeData63(t *testing.T) {
	// Define three accounts to simulate transactions with
	acc1Key, _ := crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	acc2Key, _ := crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
	acc1Addr := crypto.PubkeyToAddress(acc1Key.PublicKey)
	acc2Addr := crypto.PubkeyToAddress(acc2Key.PublicKey)

	// Create a chain generator with some simple transactions (blatantly stolen from @fjl/chain_makerts_test)
	generator := func(i int, block *core.BlockGen) {
		switch i {
		case 0:
			// In block 1, the test bank sends account #1 some ether.
			tx, _ := types.NewTransaction(block.TxNonce(testBankAddress), acc1Addr, big.NewInt(10000), params.TxGas, nil, nil).SignECDSA(testBankKey)
			block.AddTx(tx)
		case 1:
			// In block 2, the test bank sends some more ether to account #1.
			// acc1Addr passes it on to account #2.
			tx1, _ := types.NewTransaction(block.TxNonce(testBankAddress), acc1Addr, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(testBankKey)
			tx2, _ := types.NewTransaction(block.TxNonce(acc1Addr), acc2Addr, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(acc1Key)
			block.AddTx(tx1)
			block.AddTx(tx2)
		case 2:
			// Block 3 is empty but was mined by account #2.
			block.SetCoinbase(acc2Addr)
			block.SetExtra([]byte("yeehaw"))
		case 3:
			// Block 4 includes blocks 2 and 3 as uncle headers (with modified extra data).
			b2 := block.PrevBlock(1).Header()
			b2.Extra = []byte("foo")
			block.AddUncle(b2)
			b3 := block.PrevBlock(2).Header()
			b3.Extra = []byte("foo")
			block.AddUncle(b3)
		}
	}
	// Assemble the test environment
	pm := newTestProtocolManager(4, generator, nil)
	peer, _ := newTestPeer("peer", 63, pm, true)
	defer peer.close()

	// Fetch for now the entire chain db
	hashes := []common.Hash{}
	for _, key := range pm.chaindb.(*ethdb.MemDatabase).Keys() {
		hashes = append(hashes, common.BytesToHash(key))
	}
	p2p.Send(peer.app, 0x0d, hashes)
	msg, err := peer.app.ReadMsg()
	if err != nil {
		t.Fatalf("failed to read node data response: %v", err)
	}
	if msg.Code != 0x0e {
		t.Fatalf("response packet code mismatch: have %x, want %x", msg.Code, 0x0c)
	}
	var data [][]byte
	if err := msg.Decode(&data); err != nil {
		t.Fatalf("failed to decode response node data: %v", err)
	}
	// Verify that all hashes correspond to the requested data, and reconstruct a state tree
	for i, want := range hashes {
		if hash := crypto.Sha3Hash(data[i]); hash != want {
			fmt.Errorf("data hash mismatch: have %x, want %x", hash, want)
		}
	}
	statedb, _ := ethdb.NewMemDatabase()
	for i := 0; i < len(data); i++ {
		statedb.Put(hashes[i].Bytes(), data[i])
	}
	accounts := []common.Address{testBankAddress, acc1Addr, acc2Addr}
	for i := uint64(0); i <= pm.chainman.CurrentBlock().NumberU64(); i++ {
		trie := state.New(pm.chainman.GetBlockByNumber(i).Root(), statedb)

		for j, acc := range accounts {
			bw := pm.chainman.State().GetBalance(acc)
			bh := trie.GetBalance(acc)

			if (bw != nil && bh == nil) || (bw == nil && bh != nil) {
				t.Errorf("test %d, account %d: balance mismatch: have %v, want %v", i, j, bh, bw)
			}
			if bw != nil && bh != nil && bw.Cmp(bw) != 0 {
				t.Errorf("test %d, account %d: balance mismatch: have %v, want %v", i, j, bh, bw)
			}
		}
	}
}

// Tests that the transaction receipts can be retrieved based on hashes.
func TestGetReceipts63(t *testing.T) {
	// Define three accounts to simulate transactions with
	acc1Key, _ := crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	acc2Key, _ := crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
	acc1Addr := crypto.PubkeyToAddress(acc1Key.PublicKey)
	acc2Addr := crypto.PubkeyToAddress(acc2Key.PublicKey)

	// Create a chain generator with some simple transactions (blatantly stolen from @fjl/chain_makerts_test)
	generator := func(i int, block *core.BlockGen) {
		switch i {
		case 0:
			// In block 1, the test bank sends account #1 some ether.
			tx, _ := types.NewTransaction(block.TxNonce(testBankAddress), acc1Addr, big.NewInt(10000), params.TxGas, nil, nil).SignECDSA(testBankKey)
			block.AddTx(tx)
		case 1:
			// In block 2, the test bank sends some more ether to account #1.
			// acc1Addr passes it on to account #2.
			tx1, _ := types.NewTransaction(block.TxNonce(testBankAddress), acc1Addr, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(testBankKey)
			tx2, _ := types.NewTransaction(block.TxNonce(acc1Addr), acc2Addr, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(acc1Key)
			block.AddTx(tx1)
			block.AddTx(tx2)
		case 2:
			// Block 3 is empty but was mined by account #2.
			block.SetCoinbase(acc2Addr)
			block.SetExtra([]byte("yeehaw"))
		case 3:
			// Block 4 includes blocks 2 and 3 as uncle headers (with modified extra data).
			b2 := block.PrevBlock(1).Header()
			b2.Extra = []byte("foo")
			block.AddUncle(b2)
			b3 := block.PrevBlock(2).Header()
			b3.Extra = []byte("foo")
			block.AddUncle(b3)
		}
	}
	// Assemble the test environment
	pm := newTestProtocolManager(4, generator, nil)
	peer, _ := newTestPeer("peer", 63, pm, true)
	defer peer.close()

	// Collect the hashes to request, and the response to expect
	hashes := []common.Hash{}
	for i := uint64(0); i <= pm.chainman.CurrentBlock().NumberU64(); i++ {
		for _, tx := range pm.chainman.GetBlockByNumber(i).Transactions() {
			hashes = append(hashes, tx.Hash())
		}
	}
	receipts := make([]*types.Receipt, len(hashes))
	for i, hash := range hashes {
		receipts[i] = core.GetReceipt(pm.chaindb, hash)
	}
	// Send the hash request and verify the response
	p2p.Send(peer.app, 0x0f, hashes)
	if err := p2p.ExpectMsg(peer.app, 0x10, receipts); err != nil {
		t.Errorf("receipts mismatch: %v", err)
	}
}
