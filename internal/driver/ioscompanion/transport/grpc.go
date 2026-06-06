package transport

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/priyanshujain/sanderling/internal/driver/ioscompanion/companionpb"
)

// maxReceiveBytes is generous because screenshots can be large.
const maxReceiveBytes = 64 * 1024 * 1024

type grpcCompanion struct {
	conn   *grpc.ClientConn
	client pb.CompanionServiceClient
}

// Dial connects to the companion listening at address and returns a Companion.
func Dial(address string) (Companion, error) {
	conn, err := grpc.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxReceiveBytes)))
	if err != nil {
		return nil, err
	}
	return &grpcCompanion{conn: conn, client: pb.NewCompanionServiceClient(conn)}, nil
}

func (c *grpcCompanion) Close() error { return c.conn.Close() }

func (c *grpcCompanion) AccessibilityInfo(ctx context.Context) (string, error) {
	resp, err := c.client.AccessibilityInfo(ctx, &pb.AccessibilityInfoRequest{
		Format: pb.AccessibilityInfoRequest_LEGACY,
	})
	if err != nil {
		return "", err
	}
	return resp.GetJson(), nil
}

func (c *grpcCompanion) SendHID(ctx context.Context, events ...HIDEvent) error {
	stream, err := c.client.Hid(ctx)
	if err != nil {
		return err
	}
	for _, e := range events {
		if err := stream.Send(e.event); err != nil {
			return err
		}
	}
	_, err = stream.CloseAndRecv()
	return err
}

func (c *grpcCompanion) Screenshot(ctx context.Context) ([]byte, string, error) {
	resp, err := c.client.Screenshot(ctx, &pb.ScreenshotRequest{})
	if err != nil {
		return nil, "", err
	}
	return resp.GetImageData(), resp.GetImageFormat(), nil
}

func (c *grpcCompanion) Launch(ctx context.Context, bundleID string, foregroundIfRunning bool) error {
	stream, err := c.client.Launch(ctx)
	if err != nil {
		return err
	}
	err = stream.Send(&pb.LaunchRequest{Control: &pb.LaunchRequest_Start_{Start: &pb.LaunchRequest_Start{
		BundleId:            bundleID,
		ForegroundIfRunning: foregroundIfRunning,
	}}})
	if err != nil {
		return err
	}
	if _, err := stream.Recv(); err != nil && err != io.EOF {
		return err
	}
	return stream.CloseSend()
}

func (c *grpcCompanion) Terminate(ctx context.Context, bundleID string) error {
	_, err := c.client.Terminate(ctx, &pb.TerminateRequest{BundleId: bundleID})
	return err
}

func (c *grpcCompanion) Uninstall(ctx context.Context, bundleID string) error {
	_, err := c.client.Uninstall(ctx, &pb.UninstallRequest{BundleId: bundleID})
	return err
}

func (c *grpcCompanion) ListApps(ctx context.Context) ([]InstalledApp, error) {
	resp, err := c.client.ListApps(ctx, &pb.ListAppsRequest{})
	if err != nil {
		return nil, err
	}
	apps := make([]InstalledApp, 0, len(resp.GetApps()))
	for _, a := range resp.GetApps() {
		apps = append(apps, InstalledApp{
			BundleID:          a.GetBundleId(),
			Name:              a.GetName(),
			InstallType:       a.GetInstallType(),
			ProcessState:      processStateFromProto(a.GetProcessState()),
			Debuggable:        a.GetDebuggable(),
			ProcessIdentifier: a.GetProcessIdentifier(),
		})
	}
	return apps, nil
}

func (c *grpcCompanion) Install(ctx context.Context, appPath string) error {
	info, err := os.Stat(appPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("install: %s is not an app bundle directory", appPath)
	}

	stream, err := c.client.Install(ctx)
	if err != nil {
		return err
	}

	// First message sets the destination, then the payload carries the bundle
	// as a gzip-compressed tar archive the companion unpacks.
	if err := stream.Send(&pb.InstallRequest{Value: &pb.InstallRequest_Destination_{
		Destination: pb.InstallRequest_APP,
	}}); err != nil {
		return err
	}

	archive, err := tarGzipDirectory(appPath)
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.InstallRequest{Value: &pb.InstallRequest_Payload{
		Payload: &pb.Payload{Source: &pb.Payload_Data{Data: archive}},
	}}); err != nil {
		return err
	}

	if err := stream.CloseSend(); err != nil {
		return err
	}
	for {
		if _, err := stream.Recv(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// tarGzipDirectory packs dir into a gzip-compressed tar archive. Entry paths are
// relative to the parent of dir so the bundle directory itself is preserved.
func tarGzipDirectory(dir string) ([]byte, error) {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)

	base := filepath.Dir(dir)
	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(tarWriter, file)
		return err
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
