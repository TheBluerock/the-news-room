path "secret/data/newsroom/admin/*" {
  capabilities = ["read"]
}

path "secret/data/newsroom/auth/jwt_public_key" {
  capabilities = ["read"]
}
