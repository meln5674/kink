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
	"github.com/meln5674/rflag"
	"github.com/spf13/cobra"
	k8srest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var ()

// fileGatewaySendCmd represents the file-gateway send command
var fileGatewaySendCmd = &cobra.Command{
	Use:   "send [flags...] [paths...]",
	Short: "Send files to the cluster",
	Long: `Send or more files and/or directories to the cluster via the filesystem gateway. This allows using an ingress or nodeport instead of relying on kubectl cp/exec which uses the controlplane, which may be bandwidth contrained.

	The file gateway operates using the tar format, so it is analagous to using tar cz | kubectl exec -- tar xz, but does not require the tar command to be present on either side.

	The file gateway uses the same TLS setup as the api server, so data is not visible to eavesdroppers.

	File permissions (mode) are preserved, but owners and groups are not, all files will be owned by the user executing the file gateway (typically root).

	For security and simplicity reasons, only paths mounted from the shared peristence volume can be valid destinations. This also means that shared peristence must be enabled for the file gateway to be used. 
	If no file paths are specified, send expects a tar-formatted archive to be piped into standard input`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {

		ctx := context.Background()

		paths := args

		var tarStream io.Reader
		tarErrChan := make(chan error)
		if len(paths) != 0 {
			r, w, err := os.Pipe()
			if err != nil {
				return err
			}
			defer r.Close()
			go func() {
				defer w.Close()

				defer close(tarErrChan)
				tarErrChan <- writeTarArchive(&fileGatewaySendArgs, w, paths...)
			}()
			tarStream = r
		} else {
			close(tarErrChan)
			tarStream = os.Stdin
		}

		reqErr := sendToFileGateway(ctx, &fileGatewaySendArgs, &resolvedConfig, tarStream)

		tarErr := <-tarErrChan
		if reqErr == nil && tarErr == nil {
			return nil
		}
		if reqErr != nil && tarErr != nil {
			return fmt.Errorf("(While processing tarball: %v): %v", tarErr, reqErr)
		}
		if reqErr != nil {
			return reqErr
		}
		return tarErr
	},
}

type fileGatewaySendArgsT struct {
	PortForwardArgs portForwardArgsT `rflag:""`
	Dest            string           `rflag:"name=send-dest,usage=directory within the file gateway to expand into (equivalent to tar's -C)"`
	Gzip            bool             `rflag:"name=send-gzip,usage=If filepaths are provided,, compress them when sending. If reading from standard input,, expect it to be compressed. (equivalent to tar's -x)"`
	WipeDirs        bool             `rflag:"name=send-wipe-dirs,usage=Instruct the file gateway to wipe and re-create any directories that appear in the tar archive"`
	Exclude         []string         `rflag:"name=send-exclude,usage=Do not send paths which match this glob"`
	IngressURL      string           `rflag:"name=file-gateway-ingress-url,usage=If ingress is used for the file gateway,, instead use this URL,, and set the tls-server-name to the expected ingress hostname. Ignored if controlplane ingress is not used."`
	PortForward     bool             `rflag:"usage=Set up a localhost port forward for the file gateway during execution if no ingress or nodeport was set. Set to false if using a background 'kink port-forward' command. Ignored if using an ingress or nodeport for the file gateway."`
}

func (fileGatewaySendArgsT) Defaults() fileGatewaySendArgsT {
	return fileGatewaySendArgsT{
		PortForwardArgs: portForwardArgsT{}.Defaults(),
		Dest:            "/",
		PortForward:     true,
	}
}

var fileGatewaySendArgs = fileGatewaySendArgsT{}.Defaults()

func init() {
	fileGatewayCmd.AddCommand(fileGatewaySendCmd)
	rflag.MustRegister(rflag.ForPFlag(fileGatewaySendCmd.Flags()), "", &fileGatewaySendArgs)
}

func sendToFileGateway(ctx context.Context, args *fileGatewaySendArgsT, cfg *resolvedConfigT, tarStream io.Reader) error {
	tmpKubeconfigFile, err := os.CreateTemp("", "*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpKubeconfigFile.Name())
	err = fetchKubeconfig(ctx, cfg, tmpKubeconfigFile.Name())
	if err != nil {
		return err
	}
	tmpKubeconfig, err := buildCompleteKubeconfig(
		ctx, cfg,
		tmpKubeconfigFile.Name(),
		cfg.ReleaseConfig.FileGatewayHostname, "file-gateway", "file-gateway", int(cfg.ReleaseConfig.FileGatewayContainerPort),
		args.IngressURL, "",
	)
	if err != nil {
		return err
	}

	klog.V(4).Infof("Generated file gateway config: %#v", tmpKubeconfig)

	tmpRestConfig, err := clientcmd.NewDefaultClientConfig(*tmpKubeconfig, nil).ClientConfig()
	if err != nil {
		return err
	}

	if args.PortForward && tmpKubeconfig.CurrentContext == "default" {
		stopPortForward, err := startPortForward(ctx, true, &args.PortForwardArgs, cfg)
		if err != nil {
			return err
		}
		defer stopPortForward()
	}

	query := url.Values{}
	if args.Gzip {
		query.Set("gzip", "true")
	}
	if args.WipeDirs {
		query.Set("wipe-dirs", "true")
	}

	tarURLString := tmpKubeconfig.Clusters[tmpKubeconfig.Contexts[tmpKubeconfig.CurrentContext].Cluster].Server
	tarURL, err := url.Parse(tarURLString)
	if err != nil {
		panic(fmt.Sprintf("BUG: Generated kubeconfig had invalid URL %s", tarURLString))
	}

	tarURL.Path = filepath.Join(tarURL.Path, args.Dest)
	tarURL.RawQuery = query.Encode()

	client, err := k8srest.HTTPClientFor(tmpRestConfig)
	if err != nil {
		return err
	}

	resp, err := client.Post(tarURL.String(), "", tarStream)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	errMsg := strings.Builder{}
	errMsg.WriteString(fmt.Sprintf("%d %s: ", resp.StatusCode, resp.Status))
	_, err = io.Copy(&errMsg, resp.Body)
	if err != nil {
		errMsg.WriteString("<could not read response body: ")
		errMsg.WriteString(err.Error())
		errMsg.WriteString(">")
	}
	return fmt.Errorf("%s", errMsg.String())
}

func writeTarArchive(args *fileGatewaySendArgsT, w io.Writer, paths ...string) error {

	archive := tar.NewWriter(w)
	defer archive.Close()

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		err = addToArchive(args, archive, path, info)
		if err != nil {
			return err
		}
	}

	return nil
}

func addToArchive(args *fileGatewaySendArgsT, archive *tar.Writer, path string, info os.FileInfo) error {
	for _, exclude := range args.Exclude {
		matches, err := doublestar.PathMatch(exclude, path)
		if err != nil {
			return err
		}
		if matches {
			return nil
		}
	}

	// Tar always has /, this should fix windows paths
	path = strings.Join(strings.Split(path, string(filepath.Separator)), "/")

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
			err = addToArchive(args, archive, filepath.Join(path, entry.Name()), info)
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
