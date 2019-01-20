package nkn_sdk_go

func GetAddressByName(name string) (string, error) {
	var address string
	err, _ := call("getaddressbyname", map[string]interface{}{"name": name}, &address)
	if err != nil {
		return "", err
	}
	return address, nil
}

func GetSubscribers(topic string, bucket uint32) (map[string]string, error) {
	var dests map[string]string
	err, _ := call("getsubscribers", map[string]interface{}{"topic": topic, "bucket": bucket}, &dests)
	if err != nil {
		return nil, err
	}
	return dests, nil
}

func GetFirstAvailableTopicBucket(topic string) (int, error) {
	var bucket int
	err, _ := call("getfirstavailabletopicbucket", map[string]interface{}{"topic": topic}, &bucket)
	if err != nil {
		return -1, err
	}
	return bucket, nil
}

func GetTopicBucketsCount(topic string) (uint32, error) {
	var bucket uint32
	err, _ := call("gettopicbucketscount", map[string]interface{}{"topic": topic}, &bucket)
	if err != nil {
		return 0, err
	}
	return bucket, nil
}