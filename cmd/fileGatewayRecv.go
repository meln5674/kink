/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/meln5674/rflag"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// recvCmd represents the file-gateway recv command
var recvCmd = &cobra.Command{
	Use:          "recv",
	Short:        "Run an HTTP(s) server which accepts tar archives and extracts them onto specified directories",
	Long:         ``,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fileGatewayServer{args: fileGatewayRecvArgs}.ListenAndServe()
	},
}

type fileGatewayRecvArgsT struct {
	Listen      string   `rflag:"usage=Address to listen on"`
	KeyPath     string   `rflag:"usage=TLS server key file path"`
	CertPath    string   `rflag:"usage=TLS server cert file path"`
	CAPath      string   `rflag:"usage=mTLS client CA cert file path"`
	AllowedDirs []string `rflag:"usage=Allow directory to be extracted to"`
}

func (fileGatewayRecvArgsT) Defaults() fileGatewayRecvArgsT {
	return fileGatewayRecvArgsT{
		AllowedDirs: []string{},
	}
}

var fileGatewayRecvArgs = fileGatewayRecvArgsT{}.Defaults()

func (f *fileGatewayRecvArgsT) loadTLS() (*tls.Config, error) {
	// Load certificate of the CA who signed client's certificate
	pemClientCA, err := ioutil.ReadFile(f.CAPath)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemClientCA) {
		return nil, fmt.Errorf("failed to add client CA's certificate")
	}

	// Load server's certificate and private key
	serverCert, err := tls.LoadX509KeyPair(f.CertPath, f.KeyPath)
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
	}, nil
}

func init() {
	fileGatewayCmd.AddCommand(recvCmd)
	rflag.MustRegister(rflag.ForPFlag(recvCmd.Flags()), "recv-", &fileGatewayRecvArgs)
}

type fileGatewayServer struct {
	args fileGatewayRecvArgsT
}

func (f *fileGatewayServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	rootDir := req.URL.Path
	query, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Invalid query: %v", err)))
		return
	}

	wipeDirs := query.Get("wipe-dirs") == "true"
	gzipped := query.Get("compress") == "gzip"

	archiveContents := req.Body
	if gzipped {
		gzipReader, err := gzip.NewReader(archiveContents)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("Failed to read next tar header: %v", err)))
			return
		}
		defer gzipReader.Close()
		archiveContents = gzipReader
	}

	archive := tar.NewReader(archiveContents)

	extractFromArchive := func(fullpath string, header *tar.Header) error {
		switch header.Typeflag {
		case tar.TypeDir:
			if wipeDirs {
				err = os.RemoveAll(fullpath)
				if err != nil {
					return err
				}
			}
			err = os.MkdirAll(fullpath, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
		case tar.TypeReg:
			f, err := os.Create(fullpath)
			if err != nil {
				return err
			}
			defer f.Close()
			f.Chmod(os.FileMode(header.Mode))
			_, err = io.Copy(f, archive)
			if err != nil {
				return err
			}
		default:
			if err != nil {
				return fmt.Errorf("%s: Unsupported type, only directories and regular files are permitted", header.Name)
			}
		}

		return nil
	}

	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("Failed to read next tar header: %v", err)))
			return
		}
		fullpath := filepath.Clean(filepath.Join(rootDir, header.Name))
		isAllowed := false
		for _, allowed := range f.args.AllowedDirs {
			if strings.HasPrefix(fullpath, allowed) {
				isAllowed = true
				break
			}
		}
		if !isAllowed {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(fmt.Sprintf("%s is not within an allowed directory", fullpath)))
			return
		}
		err = extractFromArchive(fullpath, header)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		klog.Infof("Recv: %s %s", rootDir, header.Name)
	}

	w.Write([]byte("OK"))
}

func (f fileGatewayServer) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.Handle("/", &f)

	tlsConfig, err := f.args.loadTLS()
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:      f.args.Listen,
		TLSConfig: tlsConfig,
		Handler:   mux,
	}

	return server.ListenAndServeTLS(f.args.CertPath, f.args.KeyPath)
}
