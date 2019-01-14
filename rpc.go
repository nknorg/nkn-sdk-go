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

func GetFreeTopicBucket(topic string) (int, error) {
	var bucket int
	err := call("getfreetopicbucket", map[string]interface{}{"topic": topic}, &bucket)
	if err != nil {
		return -1, err
	}
	return bucket, nil
}

func GetTopicBucketsCount(topic string) (uint32, error) {
	var bucket uint32
	err := call("gettopicbucketscount", map[string]interface{}{"topic": topic}, &bucket)
	if err != nil {
		return 0, err
	}
	return bucket, nil
}