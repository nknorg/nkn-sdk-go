package nkn_sdk_go

func GetAddressByName(name string) (string, error) {
	var address string
	err := call("getaddressbyname", map[string]interface{}{"name": name}, &address)
	if err != nil {
		return "", err
	}
	return address, nil
}

func GetSubscribers(topic string, bucket uint32) ([]string, error) {
	var dests []string
	err := call("getsubscribers", map[string]interface{}{"topic": topic, "bucket": bucket}, &dests)
	if err != nil {
		return nil, err
	}
	return dests, nil
}