package nkn

import (
	"log"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/nknorg/nkn/v2/chain/txvalidator"
	"github.com/nknorg/nkn/v2/common"
	"github.com/nknorg/nkn/v2/config"
	"github.com/nknorg/nkn/v2/pb"
	"github.com/nknorg/nkn/v2/transaction"
)

const (
	// NanoPay will be considered expired by the sender when it's less than
	// specified amount of blocks until actual expiration.
	senderExpirationDelta = 5

	// NanoPay will be flushed on-chain when it's less than specified amount of
	// blocks until actual expiration.
	forceFlushDelta = 2

	// NanoPay will be consider expired by the receiver when it's less than
	// specified amount of blocks until actual expiration.
	receiverExpirationDelta = 3
)

// NanoPay is a nano payment channel between a payer and recipient where the
// payment amount can increase monotonically.
type NanoPay struct {
	rpcClient            rpcClient
	senderWallet         *Wallet
	recipientAddress     string
	recipientProgramHash common.Uint160
	fee                  common.Fixed64
	duration             uint32

	lock       sync.Mutex
	amount     common.Fixed64
	expiration uint32
	id         uint64
}

// NanoPayClaimer accepts NanoPay updates and send the latest state to
// blockchain periodically.
type NanoPayClaimer struct {
	rpcClient            rpcClient
	recipientAddress     string
	recipientProgramHash common.Uint160
	minFlushAmount       common.Fixed64

	lock              sync.Mutex
	amount            common.Fixed64
	closed            bool
	closeChan         chan struct{}
	expiration        uint32
	id                *uint64
	lastClaimTime     time.Time
	prevClaimedAmount common.Fixed64
	prevFlushAmount   common.Fixed64
	tx                *transaction.Transaction
}

// NewNanoPay creates a NanoPay with a rpcClient (client, multiclient or
// wallet), payer wallet, recipient wallet address, txn fee, duration in unit of
// blocks, and an optional rpc client.
func NewNanoPay(rpcClient rpcClient, senderWallet *Wallet, recipientAddress, fee string, duration int) (*NanoPay, error) {
	programHash, err := common.ToScriptHash(recipientAddress)
	if err != nil {
		return nil, err
	}

	feeFixed64, err := common.StringToFixed64(fee)
	if err != nil {
		return nil, err
	}

	np := &NanoPay{
		rpcClient:            rpcClient,
		senderWallet:         senderWallet,
		recipientAddress:     recipientAddress,
		recipientProgramHash: programHash,
		fee:                  feeFixed64,
		duration:             uint32(duration),
		id:                   randUint64(),
	}

	return np, nil
}

// Recipient returns the recipient wallet address.
func (np *NanoPay) Recipient() string {
	return np.recipientAddress
}

// IncrementAmount increments the NanoPay amount by delta and returns the signed
// NanoPay transaction. Delta is the string representation of the amount in unit
// of NKN to avoid precision loss. For example, "0.1" will be parsed as 0.1 NKN.
func (np *NanoPay) IncrementAmount(delta string) (*transaction.Transaction, error) {
	height, err := np.rpcClient.GetHeight()

	np.lock.Lock()
	defer np.lock.Unlock()

	if err != nil {
		log.Printf("Get height error: %v", err)
		if np.expiration == 0 {
			return nil, err
		}
	}

	if np.expiration == 0 || np.expiration <= uint32(height)+senderExpirationDelta {
		np.id = randUint64()
		np.expiration = uint32(height) + np.duration
		np.amount = 0
	}

	deltaValue, err := common.StringToFixed64(delta)
	if err != nil {
		return nil, err
	}

	np.amount += deltaValue
	id := np.id
	amount := np.amount
	expiration := np.expiration

	tx, err := transaction.NewNanoPayTransaction(np.senderWallet.account.ProgramHash, np.recipientProgramHash, id, amount, expiration, expiration)
	if err != nil {
		return nil, err
	}

	tx.UnsignedTx.Fee = int64(np.fee)

	if err := np.senderWallet.SignTransaction(tx); err != nil {
		return nil, err
	}

	return tx, nil
}

// NewNanoPayClaimer creates a NanoPayClaimer with a given rpcClient (client,
// multiclient or wallet), recipient wallet address, claim interval in
// millisecond, flush linger after close in millisecond, minimal flush amount,
// onError channel. It is recommended to use a positive minFlushAmount.
func NewNanoPayClaimer(rpcClient rpcClient, recipientAddress string, claimIntervalMs, lingerMs int32, minFlushAmount string, onError *OnError) (*NanoPayClaimer, error) {
	receiver, err := common.ToScriptHash(recipientAddress)
	if err != nil {
		return nil, err
	}

	minFlushAmountFixed64, err := common.StringToFixed64(minFlushAmount)
	if err != nil {
		return nil, err
	}

	npc := &NanoPayClaimer{
		rpcClient:            rpcClient,
		recipientAddress:     recipientAddress,
		recipientProgramHash: receiver,
		minFlushAmount:       minFlushAmountFixed64,
		closeChan:            make(chan struct{}),
		lastClaimTime:        time.Now(),
	}

	go func() {
		defer onError.close()
		defer func() {
			t := time.Now()
			for {
				err := npc.Flush()
				if err == nil {
					return
				}
				log.Printf("Flush nanopay txn error: %v", err)
				if time.Since(t) > time.Duration(lingerMs)*time.Millisecond {
					return
				}
				sleepTime := time.Minute
				maxSleepTime := time.Until(t.Add(time.Duration(lingerMs) * time.Millisecond))
				if sleepTime > maxSleepTime {
					sleepTime = maxSleepTime
				}
				time.Sleep(sleepTime)
			}
		}()
		for {
			time.Sleep(time.Minute)

			if npc.IsClosed() {
				return
			}

			npc.lock.Lock()
			tx := npc.tx
			lastClaimTime := npc.lastClaimTime
			expiration := npc.expiration
			incrementAmount := npc.amount - npc.prevFlushAmount
			npc.lock.Unlock()

			if tx == nil {
				continue
			}

			if incrementAmount < npc.minFlushAmount {
				continue
			}

			now := time.Now()
			if now.Before(lastClaimTime.Add(time.Duration(claimIntervalMs) * time.Millisecond)) {
				height, err := npc.rpcClient.GetHeight()
				if err != nil {
					onError.receive(err)
					continue
				}

				if expiration > uint32(height)+forceFlushDelta {
					sleepDuration := lastClaimTime.Add(time.Duration(claimIntervalMs) * time.Millisecond).Sub(now)
					if sleepDuration > time.Duration(expiration-uint32(height)-forceFlushDelta)*config.ConsensusDuration {
						sleepDuration = time.Duration(expiration-uint32(height)-forceFlushDelta) * config.ConsensusDuration
					}
					select {
					case <-time.After(sleepDuration):
						continue
					case <-npc.closeChan:
						return
					}
				}
			}

			err = npc.Flush()
			if err != nil {
				err = npc.closeWithError(err)
				onError.receive(err)
				break
			}
		}
	}()

	return npc, nil
}

// Recipient returns the NanoPayClaimer's recipient wallet address.
func (npc *NanoPayClaimer) Recipient() string {
	return npc.recipientAddress
}

func (npc *NanoPayClaimer) close() error {
	if npc.closed {
		return nil
	}
	npc.closed = true
	close(npc.closeChan)
	return nil
}

// Close closes the NanoPayClaimer.
func (npc *NanoPayClaimer) Close() error {
	npc.lock.Lock()
	defer npc.lock.Unlock()
	return npc.close()
}

// IsClosed returns whether the NanoPayClaimer is closed.
func (npc *NanoPayClaimer) IsClosed() bool {
	npc.lock.Lock()
	defer npc.lock.Unlock()
	return npc.closed
}

func (npc *NanoPayClaimer) closeWithError(err error) error {
	if err2 := npc.close(); err2 != nil {
		return multierror.Append(err, err2)
	}
	return err
}

func (npc *NanoPayClaimer) flush(force bool) error {
	if !force && npc.amount-npc.prevFlushAmount < npc.minFlushAmount {
		return nil
	}

	if npc.tx == nil {
		return nil
	}

	payload, err := transaction.Unpack(npc.tx.UnsignedTx.Payload)
	if err != nil {
		return npc.closeWithError(err)
	}

	npPayload, ok := payload.(*pb.NanoPay)
	if !ok {
		return npc.closeWithError(ErrNotNanoPay)
	}

	_, err = npc.rpcClient.SendRawTransaction(npc.tx)
	if err != nil {
		return err
	}

	npc.tx = nil
	npc.expiration = 0
	npc.lastClaimTime = time.Now()
	npc.prevFlushAmount = common.Fixed64(npPayload.Amount)

	return nil
}

// Flush sends the current latest NanoPay state to chain.
func (npc *NanoPayClaimer) Flush() error {
	npc.lock.Lock()
	defer npc.lock.Unlock()
	return npc.flush(false)
}

// Amount returns the total amount (including previously claimed and pending
// amount) of this NanoPayClaimer.
func (npc *NanoPayClaimer) Amount() *Amount {
	npc.lock.Lock()
	defer npc.lock.Unlock()
	return &Amount{npc.prevClaimedAmount + npc.amount}
}

// Claim accepts a NanoPay transaction and update NanoPay state. If the NanoPay
// in transaction has the same ID as before, it will be considered as an update
// to the previous NanoPay. If it has a different ID, it will be considered a
// new NanoPay, and previous NanoPay state will be flushed and sent to chain
// before accepting new one.
func (npc *NanoPayClaimer) Claim(tx *transaction.Transaction) (*Amount, error) {
	height, err := npc.rpcClient.GetHeight()
	if err != nil {
		return nil, err
	}

	payload, err := transaction.Unpack(tx.UnsignedTx.Payload)
	if err != nil {
		return nil, npc.closeWithError(err)
	}

	npPayload, ok := payload.(*pb.NanoPay)
	if !ok {
		return nil, npc.closeWithError(ErrNotNanoPay)
	}

	recipient, err := common.Uint160ParseFromBytes(npPayload.Recipient)
	if err != nil {
		return nil, npc.closeWithError(err)
	}

	if recipient.CompareTo(npc.recipientProgramHash) != 0 {
		return nil, npc.closeWithError(ErrWrongRecipient)
	}

	if err := txvalidator.VerifyTransaction(tx, uint32(height)); err != nil {
		return nil, npc.closeWithError(err)
	}

	sender, err := common.Uint160ParseFromBytes(npPayload.Sender)
	if err != nil {
		return nil, npc.closeWithError(err)
	}

	senderAddress, err := sender.ToAddress()
	if err != nil {
		return nil, npc.closeWithError(err)
	}

	npc.lock.Lock()
	defer npc.lock.Unlock()

	if npc.closed {
		return nil, ErrNanoPayClosed
	}

	if npc.id != nil {
		if *npc.id == npPayload.Id {
			if npc.amount >= common.Fixed64(npPayload.Amount) {
				return nil, npc.closeWithError(ErrInvalidAmount)
			}
		} else {
			if err := npc.flush(false); err != nil {
				return nil, npc.closeWithError(err)
			}
			npc.id = nil
			npc.prevClaimedAmount += npc.amount
			npc.prevFlushAmount = common.Fixed64(0)
			npc.amount = 0
		}
	}

	senderBalance, err := npc.rpcClient.BalanceByAddress(senderAddress)
	if err != nil {
		return nil, err
	}

	if senderBalance.ToFixed64()+npc.prevFlushAmount < common.Fixed64(npPayload.Amount) {
		return nil, npc.closeWithError(ErrInsufficientBalance)
	}

	if npPayload.TxnExpiration <= uint32(height)+receiverExpirationDelta {
		return nil, npc.closeWithError(ErrExpiredNanoPayTxn)
	}

	if npPayload.NanoPayExpiration <= uint32(height)+receiverExpirationDelta {
		return nil, npc.closeWithError(ErrExpiredNanoPay)
	}

	npc.tx = tx
	npc.id = &npPayload.Id
	npc.expiration = npPayload.TxnExpiration
	npc.amount = common.Fixed64(npPayload.Amount)

	return &Amount{npc.prevClaimedAmount + npc.amount}, nil
}
