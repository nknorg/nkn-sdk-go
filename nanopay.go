package nkn

import (
	"sync"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/nknorg/nkn/chain"
	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/pb"
	"github.com/nknorg/nkn/transaction"
	"github.com/nknorg/nkn/util/config"
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
	recipientAddress     string
	recipientProgramHash common.Uint160
	rpcConfig            RPCConfigInterface

	lock              sync.Mutex
	amount            common.Fixed64
	closed            bool
	expiration        uint32
	id                *uint64
	lastClaimTime     time.Time
	prevClaimedAmount common.Fixed64
	tx                *transaction.Transaction
}

// NewNanoPay creates a NanoPay with a payer wallet, recipient wallet address,
// txn fee, and duration in unit of blocks.
func NewNanoPay(senderWallet *Wallet, recipientAddress, fee string, duration int) (*NanoPay, error) {
	programHash, err := common.ToScriptHash(recipientAddress)
	if err != nil {
		return nil, err
	}

	feeFixed64, err := common.StringToFixed64(fee)
	if err != nil {
		return nil, err
	}

	np := &NanoPay{
		senderWallet:         senderWallet,
		recipientAddress:     recipientAddress,
		recipientProgramHash: programHash,
		fee:                  feeFixed64,
		duration:             uint32(duration),
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
	height, err := np.senderWallet.GetHeight()
	if err != nil {
		return nil, err
	}

	np.lock.Lock()
	if np.expiration == 0 || np.expiration <= uint32(height)+senderExpirationDelta {
		np.id = randUint64()
		np.expiration = uint32(height) + np.duration
		np.amount = 0
	}

	deltaValue, err := common.StringToFixed64(delta)
	if err != nil {
		np.lock.Unlock()
		return nil, err
	}

	np.amount += deltaValue
	id := np.id
	amount := np.amount
	expiration := np.expiration
	np.lock.Unlock()

	tx, err := transaction.NewNanoPayTransaction(np.senderWallet.account.ProgramHash, np.recipientProgramHash, id, amount, expiration, expiration)
	if err != nil {
		return nil, err
	}

	tx.UnsignedTx.Fee = int64(np.fee)

	if err := np.senderWallet.signTransaction(tx); err != nil {
		return nil, err
	}

	return tx, nil
}

// NewNanoPayClaimer creates a NanoPayClaimer with a given recipient wallet
// address, claim interval in millisecond, onError channel, and an optional
// rpcConfig that is used for making RPC requests.
func NewNanoPayClaimer(recipientAddress string, claimIntervalMs int32, onError *OnError, rpcConfig RPCConfigInterface) (*NanoPayClaimer, error) {
	receiver, err := common.ToScriptHash(recipientAddress)
	if err != nil {
		return nil, err
	}

	npc := &NanoPayClaimer{
		recipientAddress:     recipientAddress,
		recipientProgramHash: receiver,
		rpcConfig:            rpcConfig,
	}

	go func() {
		for {
			time.Sleep(time.Second)

			if npc.closed {
				break
			}

			npc.lock.Lock()
			tx := npc.tx
			lastClaimTime := npc.lastClaimTime
			expiration := npc.expiration
			npc.lock.Unlock()

			if tx == nil {
				continue
			}

			now := time.Now()
			if now.Before(npc.lastClaimTime.Add(time.Duration(claimIntervalMs) * time.Millisecond)) {
				height, err := GetHeight(npc.rpcConfig)
				if err != nil {
					onError.receive(err)
					continue
				}

				if expiration > uint32(height)+forceFlushDelta {
					sleepDuration := lastClaimTime.Add(time.Duration(claimIntervalMs) * time.Millisecond).Sub(now)
					if sleepDuration > time.Duration(expiration-uint32(height)-forceFlushDelta)*config.ConsensusDuration {
						sleepDuration = time.Duration(expiration-uint32(height)-forceFlushDelta) * config.ConsensusDuration
					}
					time.Sleep(sleepDuration)
					continue
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
	return npc.flush()
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

func (npc *NanoPayClaimer) flush() error {
	if npc.tx == nil {
		return nil
	}

	_, err := SendRawTransaction(npc.tx, npc.rpcConfig)
	if err != nil {
		return err
	}

	npc.tx = nil
	npc.expiration = 0
	npc.lastClaimTime = time.Now()

	return nil
}

// Flush sends the current latest NanoPay state to chain.
func (npc *NanoPayClaimer) Flush() error {
	npc.lock.Lock()
	defer npc.lock.Unlock()
	return npc.flush()
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
	height, err := GetHeight(npc.rpcConfig)
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

	if err := chain.VerifyTransaction(tx, 0); err != nil {
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

	senderBalance, err := GetBalance(senderAddress, npc.rpcConfig)
	if err != nil {
		return nil, err
	}

	npc.lock.Lock()
	defer npc.lock.Unlock()

	if npc.closed {
		return nil, ErrNanoPayClosed
	}

	if npc.id == nil || *npc.id == npPayload.Id {
		if senderBalance.ToFixed64() < npc.amount {
			return nil, npc.closeWithError(ErrInsufficientBalance)
		}
	}

	if npc.id != nil {
		if *npc.id == npPayload.Id {
			if npc.amount >= common.Fixed64(npPayload.Amount) {
				return nil, npc.closeWithError(ErrInvalidAmount)
			}
		} else {
			if err := npc.flush(); err != nil {
				return nil, npc.closeWithError(err)
			}
			npc.id = nil
			npc.prevClaimedAmount += npc.amount
			npc.amount = -1
		}
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
