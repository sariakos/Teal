package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// GenerateSSHKeyPair returns a new ed25519 deploy key:
//
//   - privatePEM is the OpenSSH private key (no passphrase) in standard PEM
//     wrapping. Suitable for `ssh -i <file>`.
//   - publicSSH is the single-line OpenSSH public key
//     ("ssh-ed25519 <base64> teal@<slug>"). Paste-into-GitHub format.
//
// We choose ed25519 over RSA for: smaller keys, faster operations, simpler
// generation. GitHub has supported ed25519 deploy keys since 2017.
func GenerateSSHKeyPair(label string) (privatePEM []byte, publicSSH string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("git: ed25519 generate: %w", err)
	}

	privBlock, err := encodeOpenSSHPrivate(priv, pub, label)
	if err != nil {
		return nil, "", err
	}
	privatePEM = pem.EncodeToMemory(&pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: privBlock,
	})

	publicSSH = "ssh-ed25519 " + base64.StdEncoding.EncodeToString(encodeSSHEd25519PublicWire(pub)) + " teal@" + label
	return privatePEM, publicSSH, nil
}

// PublicKeyFromPrivatePEM extracts the OpenSSH public-key line from a
// previously-generated private key PEM. Lets the API surface the public
// key without storing a redundant column — given the private, the public
// is deterministic.
func PublicKeyFromPrivatePEM(privatePEM []byte, label string) (string, error) {
	signer, err := ssh.ParsePrivateKey(privatePEM)
	if err != nil {
		return "", fmt.Errorf("git: parse private key: %w", err)
	}
	pub := signer.PublicKey()
	if pub.Type() != "ssh-ed25519" {
		return "", fmt.Errorf("git: expected ssh-ed25519, got %s", pub.Type())
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(pub.Marshal()) + " teal@" + label, nil
}

// SSHKeyFingerprint returns the SHA-256 fingerprint string GitHub displays
// for a deploy key, e.g. "SHA256:Nq...". Useful for the API to expose
// alongside the public key.
func SSHKeyFingerprint(publicSSH string) (string, error) {
	// The format is "ssh-ed25519 <base64-blob> [comment]". Decode the blob
	// and SHA-256 it.
	var blob []byte
	for i := 0; i < len(publicSSH); i++ {
		if publicSSH[i] == ' ' {
			rest := publicSSH[i+1:]
			end := len(rest)
			if sp := indexByte(rest, ' '); sp >= 0 {
				end = sp
			}
			b, err := base64.StdEncoding.DecodeString(rest[:end])
			if err != nil {
				return "", fmt.Errorf("git: decode public key: %w", err)
			}
			blob = b
			break
		}
	}
	if blob == nil {
		return "", fmt.Errorf("git: malformed public key line")
	}
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

// encodeOpenSSHPrivate produces the binary body of an OpenSSH ed25519
// private key, ready to be PEM-wrapped. Format reference: PROTOCOL.key in
// the OpenSSH source. We omit ciphering (the spec calls "none" cipher with
// an empty KDF).
func encodeOpenSSHPrivate(priv ed25519.PrivateKey, pub ed25519.PublicKey, comment string) ([]byte, error) {
	const magic = "openssh-key-v1\x00"

	var pubBuf, privBuf []byte
	pubBuf = appendOpenSSHEd25519Public(pubBuf, pub)

	// Random 4-byte check int twice (per spec).
	var check [4]byte
	if _, err := rand.Read(check[:]); err != nil {
		return nil, err
	}
	privBuf = append(privBuf, check[:]...)
	privBuf = append(privBuf, check[:]...)
	privBuf = appendString(privBuf, "ssh-ed25519")
	privBuf = appendBytes(privBuf, pub) // public part (32 bytes)
	privBuf = appendBytes(privBuf, []byte(priv)) // private part (64 bytes)
	privBuf = appendString(privBuf, comment)
	// Pad to 8-byte boundary as spec requires.
	for i := byte(1); len(privBuf)%8 != 0; i++ {
		privBuf = append(privBuf, i)
	}

	out := []byte(magic)
	out = appendString(out, "none")          // cipher
	out = appendString(out, "none")          // kdf
	out = appendString(out, "")              // kdf options
	out = appendUint32(out, 1)               // num keys
	out = appendBytes(out, pubBuf)           // public part as ssh-string
	out = appendBytes(out, privBuf)          // private part (encrypted body if cipher!=none)
	return out, nil
}

// encodeSSHEd25519PublicWire produces the SSH wire encoding of an ed25519
// public key (used in the public-key file's base64 blob).
func encodeSSHEd25519PublicWire(pub ed25519.PublicKey) []byte {
	return appendOpenSSHEd25519Public(nil, pub)
}

func appendOpenSSHEd25519Public(out []byte, pub ed25519.PublicKey) []byte {
	out = appendString(out, "ssh-ed25519")
	out = appendBytes(out, pub)
	return out
}

// SSH wire helpers — uint32-length-prefixed strings/blobs.
func appendUint32(out []byte, v uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	return append(out, buf[:]...)
}
func appendString(out []byte, s string) []byte { return appendBytes(out, []byte(s)) }
func appendBytes(out []byte, b []byte) []byte {
	out = appendUint32(out, uint32(len(b)))
	return append(out, b...)
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
