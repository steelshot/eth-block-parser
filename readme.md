# Ethereum Block Parser

## Assignment

### Goal
Implement Ethereum blockchain parser that will allow to query transactions for subscribed
addresses.

### Problem
Users not able to receive push notifications for incoming/outgoing transactions. By
Implementing Parser interface we would be able to hook this up to notifications service to
notify about any incoming/outgoing transactions.

### Limitations
- Use Go Language
- Avoid usage of external libraries
- Use Ethereum JSONRPC to interact with Ethereum Blockchain
- Use memory storage for storing any data (should be easily extendable to support any
storage in the future)

Expose public interface for external usage either via code or command line or rest api that will
include supported list of operations defined in the Parser interface

```go
type Parser interface {
    // last parsed block
    GetCurrentBlock() int
    // add address to observer
    Subscribe(address string) bool
    // list of inbound or outbound transactions for an address
    GetTransactions(address string) []Transaction
}
```

## Approaches
I have had no experience working with any blockchain yet, so I'm making lots of assumptions or educated guesses here. Also the Eth JSON RPC documentation is not the greatest.

### 1. Collect transactions on the go using periodic background block polling
The following (currently used) approach uses periodic block polling. Essentially;
1. At an interval in a goroutine, we check if a newer block exists, and we capture its transaction hashes.
2. On a separate goroutine, once we have hashes to process, we fetch transactions to match those hashes
3. On a final goroutine, for each concrete transaction we assign it to any subscribed addresses based on **To** and **From** address fields.
4. Once called, return the cached Transaction for the given address.

The following approach does have significant drawbacks:
1. This could be problematic if the endpoint used is strictly rate limited, so after a few blocks we quickly start getting HTTP 429 errors, since some blocks have significant transaction counts.
   - Alternative endpoint should be used
2. We are using on demand polling, to not miss any new blocks, we poll quite frequently, however, it is still most likely a possibility that we could miss a block.
   - Verification is required to run the Parser for a very long duration and verify if any blocks have been missed
   - It would be straightforward to replace on demand polling with a subscription-based service like WebSockets or Kafka, 
     instead of us asking for data; we get it sent over once it is available. I couldn't find documentation on such capabilities, at least nothing without third party tools/APIs.
3. There seems to be no RPC to get transactions in a batch. We could reduce individual hash to tx conversion with just one single batch RPC call.
4. This is potentially very memory hungry if we are not careful if an address is doing many transactions, before we unsubscribe.

### 2. Fetching all transactions on demand
The following approach;
1. Once a subscription is made, the address is associated with the latest block at the time of the subscription.
2. Once the GetTransactions is called (on demand), at that moment we would capture the latest block.
3. Since we have two blocks know, we can iterate through all the blocks and capture their hashes.
4. Using all the hashes, fetch transactions to match those hashes.
5. Finally, associate any transactions to the given address.

The following approach also does have significant drawbacks;
1. We are still affected by rate-limited endpoints.
2. Still lacking a batch transaction RPC.
3. While there is no more periodic polling, for every GetTransactions call, we would be re-fetching all the hashes/transactions again.
   - We could avoid duplicate hash/tx lookups, by caching previous results, and only fetching the difference from the latest block and the block from the last GetTransactions call.


## Notes, Assumptions, Doubts or Improvements
- Assumption: GetCurrentBlock should return the last parsed block by this Parser rather than the latest block (eth_blockNumber).
- Assumption: Couldn't find it, but nothing documents the expected HTTP status codes, so anything not 200 is invalid.
- Notes: While the JSON RPCs allow specifying an ID to associate responses with, I did not use them, as we are sending requests and getting their responses in a synchronised manner.
- Doubt: When subscribing, I could not find a reliable way (via the RPC docs) to verify if an address is actually an existing address
  - Most RPCs return 0x0 when dealing with accounts. There is no concrete docs saying what 0x0 means for individual RPCs
  - I am checking whether the address is 42 characters long and is a valid hex string.
- Improvement: The problem defines that the GetCurrentBlock() function should use default int.
  An ideal solution would be to use big.Int since Eth JSON RPC uses 256bit integers represented as hex (from my understanding), however, to keep things simpler uint64
  should fully suffice for this example and works better with the std library packages (casting without over/underflow checks), since most numeric types use a concrete size **uint64 vs uint** or **int64 vs int**.
- Improvement: None of the interface functions have any error checking, being idiomatic to Go and effective API handling, I've added error handling to Subscribe and GetTransactions functions.
- Notes: https://eth.public-rpc.com is not so strictly rate-limited
