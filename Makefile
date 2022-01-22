GOMOBILE=gomobile
GOBIND=$(GOMOBILE) bind
BUILDDIR=$(shell pwd)/build
IMPORT_PATH=nkn
LDFLAGS='-s -w'
ANDROID_LDFLAGS='-s -w'

ANDROID_BUILDDIR=$(BUILDDIR)/android
ANDROID_ARTIFACT=$(ANDROID_BUILDDIR)/nkn.aar
IOS_BUILDDIR=$(BUILDDIR)/ios
IOS_ARTIFACT=$(IOS_BUILDDIR)/Nkn.xcframework

BUILD_PACKAGE=github.com/nknorg/nkn-sdk-go github.com/nknorg/ncp-go github.com/nknorg/nkn/v2/transaction github.com/nknorg/nkngomobile
ANDROID_BUILD_CMD="$(GOBIND) -a -ldflags $(ANDROID_LDFLAGS) -target=android -o $(ANDROID_ARTIFACT) $(BUILD_PACKAGE)"
IOS_BUILD_CMD="$(GOBIND) -a -ldflags $(LDFLAGS) -target=ios -o $(IOS_ARTIFACT) $(BUILD_PACKAGE)"

.PHONY: test
test:
	go test ./...

.PHONY: pb
pb:
	protoc --go_out=. payloads/*.proto

define build
	mkdir -p $(1)
	eval $(2)
endef

.PHONY: mobile android ios clean

mobile: android ios

android:
	$(call build,$(ANDROID_BUILDDIR),$(ANDROID_BUILD_CMD))

ios:
	$(call build,$(IOS_BUILDDIR),$(IOS_BUILD_CMD))

clean:
	rm -rf $(BUILDDIR)
