make build
sudo systemctl stop softrains-servo-rpc.service
mv bin/gimbalrpc-arm64 ~/servo-rpc
sudo systemctl restart softrains-servo-rpc.service
journalctl -u softrains-servo-rpc.service -f
