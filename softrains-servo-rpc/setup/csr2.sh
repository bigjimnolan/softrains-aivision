cat > softrainsvision-servos-rpc2.local-openssl.cnf <<'EOF'
[ req ] 
default_bits = 2048
prompt = no 
default_md = sha384
distinguished_name = dn
req_extensions = req_ext

[ dn ]
CN = softrainsvision-servos-rpc2.local

[ req_ext ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = softrainsvision-servos-rpc2.local
DNS.2 = orangebot.local
IP.1 = 192.168.68.85
EOF

openssl req -new -nodes -newkey rsa:2048 \
   -keyout softrainsvision-servos-rpc.local.key \
   -out softrainsvision-servos-rpc2.local.csr \
   -config softrainsvision-servos-rpc2.local-openssl.cnf

