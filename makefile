
all: windows linux macos

windows:
	GOOS=windows go build -o build/windows/mihomo-tui.exe -ldflags="-s -w" -trimpath main.go

linux:
	GOOS=linux go build -o build/linux/mihomo-tui -ldflags="-s -w" -trimpath main.go

macos:
	GOOS=darwin go build -o build/macos/mihomo-tui -ldflags="-s -w" -trimpath main.go