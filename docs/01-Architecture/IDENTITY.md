# Cryptographic Identity

This repository uses Ed25519 (EdDSA) GnuPG signatures to ensure code integrity and developer authenticity.

## Developer Information
- **Name:** Zhou Wei
- **Email:** zhouwei6160@gmail.com
- **GPG Fingerprint:** `FFA0B998E7AF2A9A9A2C6177F96525FE58575DCF`
- **Key Type:** Ed25519 (Modern, Web3-standard EdDSA)

## Verification
To verify the signatures in this repository, import the public key:
```bash
gpg --import PUBLIC_KEY.asc
```

To verify the README signature:
```bash
gpg --verify README.md.asc README.md
```
