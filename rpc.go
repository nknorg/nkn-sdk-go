package nkn

import (
	"encoding/json"
	"errors"

	"github.com/nknorg/nkn/api/httpjson/client"
)

func call(address string, action string, params map[string]interface{}, result interface{}) (error, int32) {
	data, err := client.Call(address, action, 0, params)
	resp := make(map[string]*json.RawMessage)
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return err, -1
	}
	if resp["error"] != nil {
		errResp := &struct {
			Code    int32
			Message string
			Data    string
		}{}
		err := json.Unmarshal(*resp["error"], &errResp)
		if err != nil {
			return err, -1
		}
		code := errResp.Code
		if code < 0 {
			code = -1
		}
		msg := errResp.Message
		if len(errResp.Data) > 0 {
			msg += ": " + errResp.Data
		}
		return errors.New(msg), code
	}

	err = json.Unmarshal(*resp["result"], result)
	if err != nil {
		return err, 0
	}
	return nil, 0
}
