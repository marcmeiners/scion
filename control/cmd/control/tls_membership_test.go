package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/scionproto/scion/control/beaconing"
	cstrust "github.com/scionproto/scion/control/trust"
	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/scrypto/cppki"
	privtrust "github.com/scionproto/scion/private/trust"
)

type testSignerGen struct {
	signer privtrust.Signer
}

func (g testSignerGen) Generate(context.Context) ([]privtrust.Signer, error) {
	return []privtrust.Signer{g.signer}, nil
}

type recordingConnDialer struct {
	ctx context.Context
}

func (d *recordingConnDialer) Dial(ctx context.Context, _ net.Addr) (net.Conn, error) {
	d.ctx = ctx
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return c1, nil
}

func TestMembershipConnDialerAndClientCertSelection(t *testing.T) {
	publicIA := addr.MustParseIA("1-ff00:0:110")
	privateIA := addr.MustParseIA("4096-ff00:0:110")

	loaders := newIALoaders(
		publicIA,
		nil,
		map[addr.IA]cstrust.TLSCertificateLoader{
			publicIA:  testTLSLoader(t, "public-cert", publicIA),
			privateIA: testTLSLoader(t, "private-cert", privateIA),
		},
	)

	// verify membershipConnDialer writes IA into ctx
	base := &recordingConnDialer{}
	dialer := membershipConnDialer{base: base, ia: privateIA}
	conn, err := dialer.Dial(context.Background(), &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	_ = conn.Close()

	ctxIA, ok := beaconing.LocalIAFromContext(base.ctx)
	require.True(t, ok)
	require.Equal(t, privateIA, ctxIA)

	// verify selecting by membership IA chooses matching cert
	cert, err := loaders.clientCertificate(base.ctx, ctxIA)
	require.NoError(t, err)
	require.NotNil(t, cert.Leaf)
	require.Equal(t, "private-cert", cert.Leaf.Subject.CommonName)

	// verify absent IA falls back to default/public cert
	fallbackCert, err := loaders.GetClientCertificate(&tls.CertificateRequestInfo{})
	require.NoError(t, err)
	require.NotNil(t, fallbackCert.Leaf)
	require.Equal(t, "public-cert", fallbackCert.Leaf.Subject.CommonName)
}

func TestIALoadersServerCertSelectionBySNI(t *testing.T) {
	publicIA := addr.MustParseIA("1-ff00:0:110")
	privateIA := addr.MustParseIA("4096-ff00:0:110")

	loaders := newIALoaders(
		publicIA,
		map[addr.IA]cstrust.TLSCertificateLoader{
			publicIA:  testTLSLoader(t, "public-server-cert", publicIA),
			privateIA: testTLSLoader(t, "private-server-cert", privateIA),
		},
		nil,
	)

	// matching SNI IA should select the private membership server certificate
	privateCert, err := loaders.GetCertificate(&tls.ClientHelloInfo{
		ServerName: privateIA.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, privateCert.Leaf)
	require.Equal(t, "private-server-cert", privateCert.Leaf.Subject.CommonName)

	// missing/invalid SNI should fall back to default (public) certificate
	fallbackCert, err := loaders.GetCertificate(&tls.ClientHelloInfo{
		ServerName: "not-an-ia",
	})
	require.NoError(t, err)
	require.NotNil(t, fallbackCert.Leaf)
	require.Equal(t, "public-server-cert", fallbackCert.Leaf.Subject.CommonName)
}

func testTLSLoader(t *testing.T, cn string, ia addr.IA) cstrust.TLSCertificateLoader {
	t.Helper()
	return cstrust.TLSCertificateLoader{
		SignerGen: testSignerGen{signer: testSigner(t, cn, ia)},
	}
}

// creates a fake signer object with a fresh key and self-signed cert
func testSigner(t *testing.T, cn string, ia addr.IA) privtrust.Signer {
	t.Helper()
	now := time.Now()
	notBefore := now.Add(-time.Minute)
	notAfter := now.Add(time.Hour)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	require.NoError(t, err)
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		SubjectKeyId: []byte{0x01, 0x02, 0x03, 0x04},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return privtrust.Signer{
		PrivateKey:   key,
		IA:           ia,
		Chain:        []*x509.Certificate{cert},
		Subject:      cert.Subject,
		SubjectKeyID: cert.SubjectKeyId,
		Expiration:   cert.NotAfter,
		TRCID: cppki.TRCID{
			ISD:    ia.ISD(),
			Base:   1,
			Serial: 1,
		},
		ChainValidity: cppki.Validity{
			NotBefore: cert.NotBefore,
			NotAfter:  cert.NotAfter,
		},
	}
}
