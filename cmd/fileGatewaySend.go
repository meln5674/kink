/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	k8srest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	fileGatewaySendDest       string
	fileGatewaySendGzip       bool
	fileGatewaySendWipeDirs   bool
	fileGatewaySendExclude    []string
	fileGatewaySendIngressURL string
)

// sendCmd represents the send command
var sendCmd = &cobra.Command{
	Use:   "send [flags...] [paths...]",
	Short: "Send files to the cluster",
	Long: `Send or more files and/or directories to the cluster via the filesystem gateway. This allows using an ingress or nodeport instead of relying on kubectl cp/exec which uses the controlplane, which may be bandwidth contrained.

	The file gateway operates using the tar format, so it is analagous to using tar cz | kubectl exec -- tar xz, but does not require the tar command to be present on either side.

	The file gateway uses the same TLS setup as the api server, so data is not visible to eavesdroppers.

	File permissions (mode) are preserved, but owners and groups are not, all files will be owned by the user executing the file gateway (typically root).

	For security and simplicity reasons, only paths mounted from the shared peristence volume can be valid destinations. This also means that shared peristence must be enabled for the file gateway to be used. 
	If no file paths are specified, send expects a tar-formatted archive to be piped into standard input`,
	RunE: func(cmd *cobra.Command, args []string) error {

		ctx := context.Background()

		dest := fileGatewaySendDest
		if dest == "" {
			dest = "/"
		}

		tmpKubeconfigFile, err := os.CreateTemp("", "*")
		if err != nil {
			return err
		}
		// defer os.Remove(tmpKubeconfigFile.Name())
		err = fetchKubeconfig(ctx, tmpKubeconfigFile.Name())
		if err != nil {
			return err
		}
		err = buildCompleteKubeconfig(ctx, tmpKubeconfigFile.Name())
		if err != nil {
			return err
		}

		overrides := &clientcmd.ConfigOverrides{}

		var tarHost string

		if !releaseConfig.ControlplaneIsNodePort && fileGatewaySendIngressURL != "" {
			overrides.ClusterInfo.TLSServerName = releaseConfig.FileGatewayHostname
			overrides.ClusterInfo.Server = fileGatewaySendIngressURL
		}

		tmpKubeconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{
				ExplicitPath: tmpKubeconfigFile.Name(),
			},
			overrides,
		).ClientConfig()

		klog.V(4).Infof("Generated file gateway config: %#v", tmpKubeconfig)

		client, err := k8srest.HTTPClientFor(tmpKubeconfig)
		if err != nil {
			return err
		}

		query := url.Values{}
		if fileGatewaySendGzip {
			query.Set("gzip", "true")
		}
		if fileGatewaySendWipeDirs {
			query.Set("wipe-dirs", "true")
		}

		var tarURL *url.URL

		if releaseConfig.ControlplaneIsNodePort {
			k8sClient, err := kubernetes.NewForConfig(kubeconfig)
			if err != nil {
				return err
			}
			port, err := getNodePort(ctx, k8sClient, releaseNamespace, releaseConfig.ControlplaneFullname, "file-gateway", "file gateway")
			if err != nil {
				return fmt.Errorf("Could not retrieve controlplane service to get nodePort")
			}
			if port == 0 {
				return fmt.Errorf("Controlplane service has not been assigned a NodePort yet")
			}
			tarHost = fmt.Sprintf("%s:%d", releaseConfig.FileGatewayHostname, port)
		} else if fileGatewaySendIngressURL != "" {
			parsed, err := url.Parse(fileGatewaySendIngressURL)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("parsing url %s", fileGatewaySendIngressURL))
			}
			tarHost = parsed.Host
		} else {
			tarHost = releaseConfig.FileGatewayHostname
		}

		tarURL = &url.URL{
			Scheme:   "https",
			Host:     tarHost,
			Path:     dest,
			RawQuery: query.Encode(),
		}

		paths := args

		var tarStream io.Reader
		tarErrChan := make(chan error)
		if len(args) != 0 {
			r, w, err := os.Pipe()
			if err != nil {
				return err
			}
			defer r.Close()
			go func() {
				defer w.Close()

				defer close(tarErrChan)

				tarErrChan <- func() error {

					archive := tar.NewWriter(w)
					defer archive.Close()

					var addToArchive func(string, os.FileInfo) error

					addToArchive = func(path string, info os.FileInfo) error {
						for _, exclude := range fileGatewaySendExclude {
							matches, err := doublestar.PathMatch(exclude, path)
							if err != nil {
								return err
							}
							if matches {
								return nil
							}
						}

						var flag byte
						mode := info.Mode()
						if mode&fs.ModeDir != 0 {
							flag = tar.TypeDir
						} else if mode&(fs.ModeSymlink|fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeCharDevice|fs.ModeIrregular) != 0 {
							return fmt.Errorf("%s: Unsupported file type", path)
						} else {
							flag = tar.TypeReg
						}
						klog.V(4).Infof("Sending: %s", path)
						archive.WriteHeader(&tar.Header{
							Typeflag: flag,
							Name:     path,
							Size:     info.Size(),
							Mode:     int64(info.Mode()),
							ModTime:  info.ModTime(),
						})

						if info.IsDir() {
							entries, err := os.ReadDir(path)
							if err != nil {
								return err
							}

							for _, entry := range entries {
								info, err := entry.Info()
								if err != nil {
									return err
								}
								// TODO: Replace this recursion with a queue
								err = addToArchive(filepath.Join(path, entry.Name()), info)
								if err != nil {
									return err
								}
							}
						} else {
							f, err := os.Open(path)
							if err != nil {
								return err
							}
							_, err = io.Copy(archive, f)
							if err != nil {
								return err
							}
						}
						klog.V(4).Infof("Sent: %s", path)

						return nil
					}

					for _, path := range paths {
						info, err := os.Stat(path)
						if err != nil {
							return err
						}
						err = addToArchive(path, info)
						if err != nil {
							return err
						}
					}

					return nil
				}()
			}()
			tarStream = r
		} else {
			close(tarErrChan)
			tarStream = os.Stdin
		}
		resp, err := client.Post(tarURL.String(), "", tarStream)
		if err != nil {
			return err
		}
		tarErr := <-tarErrChan
		if resp.StatusCode == http.StatusOK {
			if tarErr != nil {
				return tarErr
			}
			return nil
		}
		errMsg := strings.Builder{}
		errMsg.WriteString(resp.Status)
		_, err = io.Copy(&errMsg, resp.Body)
		if err != nil {
			errMsg.WriteString("<could not read response body: ")
			errMsg.WriteString(err.Error())
			errMsg.WriteString(">")
		}
		if tarErr != nil {
			return fmt.Errorf("(While processing tarball: %v): %s", tarErr, errMsg.String())
		}
		return fmt.Errorf("%s", errMsg)
	},
}

func init() {
	fileGatewayCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVar(&fileGatewaySendDest, "send-dest", "", "directory within the file gateway to expand into (equivalent to tar's -C)")
	sendCmd.Flags().BoolVar(&fileGatewaySendGzip, "send-gzip", false, "If filepaths are provided, compress them when sending. If reading from standard input, expect it to be compressed. (equivalent to tar's -x)")
	sendCmd.Flags().StringArrayVar(&fileGatewaySendExclude, "send-exclude", []string{}, "Do not send paths which match this glob")
	sendCmd.Flags().BoolVar(&fileGatewaySendWipeDirs, "send-wipe-dirs", false, "Instruct the file gateway to wipe and re-create any directories that appear in the tar archive")
	sendCmd.Flags().StringVar(&fileGatewaySendIngressURL, "file-gateway-ingress-url", "", "If ingress is used for the file gateway, instead use this URL, and set the tls-server-name to the expected ingress hostname. Ignored if controlplane ingress is not used.")
}
