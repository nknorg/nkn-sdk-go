package nkn_sdk_go

import (
	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/transaction"
)

type NanoPay struct {
	w        *WalletSDK
	receiver common.Uint160
	id       uint64
}

func NewNanoPay(w *WalletSDK, receiver common.Uint160) *NanoPay {
	return &NanoPay{w, receiver, randUint64()}
}

func (np *NanoPay) Send(value string, txnExpiration, nanoPayExpiration uint32) (string, error) {
	outputValue, err := common.StringToFixed64(value)
	if err != nil {
		return "", err
	}
	height, err := np.w.getHeight()
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewNanoPayTransaction(np.w.account.ProgramHash, np.receiver, np.id, outputValue, height+txnExpiration, height+nanoPayExpiration)
	if err != nil {
		return "", err
	}

	id, err, _ := np.w.sendRawTransaction(tx)
	return id, err
}
