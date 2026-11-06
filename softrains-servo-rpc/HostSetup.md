# Install Golang
wget https://go.dev/dl/go1.27.0.linux-arm64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.27.0.linux-arm64.tar.gz
## add to bashrc
export PATH="$PATH:$(go env GOPATH)/bin"