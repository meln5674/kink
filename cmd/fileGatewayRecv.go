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

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	fileGatewayRecvListen      string
	fileGatewayRecvKeyPath     string
	fileGatewayRecvCertPath    string
	fileGatewayRecvCAPath      string
	fileGatewayRecvAllowedDirs []string
)

func loadTLS() (*tls.Config, error) {
	// Load certificate of the CA who signed client's certificate
	pemClientCA, err := ioutil.ReadFile(fileGatewayRecvCAPath)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemClientCA) {
		return nil, fmt.Errorf("failed to add client CA's certificate")
	}

	// Load server's certificate and private key
	serverCert, err := tls.LoadX509KeyPair(fileGatewayRecvCertPath, fileGatewayRecvKeyPath)
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

// recvCmd represents the file-gateway recv command
var recvCmd = &cobra.Command{
	Use:   "recv",
	Short: "Run an HTTP(s) server which accepts tar archives and extracts them onto specified directories",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {

		http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
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
				for _, allowed := range fileGatewayRecvAllowedDirs {
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
		})

		tlsConfig, err := loadTLS()
		if err != nil {
			return err
		}

		server := &http.Server{
			Addr:      fileGatewayRecvListen,
			TLSConfig: tlsConfig,
		}

		return server.ListenAndServeTLS(fileGatewayRecvCertPath, fileGatewayRecvKeyPath)
	},
}

func init() {
	fileGatewayCmd.AddCommand(recvCmd)

	recvCmd.Flags().StringVar(&fileGatewayRecvListen, "recv-listen", ":8443", "Address to listen on")
	recvCmd.Flags().StringVar(&fileGatewayRecvKeyPath, "recv-key", "", "TLS server key file path")
	recvCmd.Flags().StringVar(&fileGatewayRecvCertPath, "recv-cert", "", "TLS server cert file path")
	recvCmd.Flags().StringVar(&fileGatewayRecvCAPath, "recv-ca", "", "mTLS client CA cert file path")
	recvCmd.Flags().StringArrayVar(&fileGatewayRecvAllowedDirs, "recv-allowed-dir", []string{}, "Allow directory to be extracted to")
}
