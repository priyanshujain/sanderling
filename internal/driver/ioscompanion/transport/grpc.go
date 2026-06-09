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

func (c *grpcCompanion) Describe(ctx context.Context) (ScreenDescription, error) {
	resp, err := c.client.Describe(ctx, &pb.TargetDescriptionRequest{})
	if err != nil {
		return ScreenDescription{}, err
	}
	return screenDescriptionFrom(resp), nil
}

// screenDescriptionFrom extracts the point dimensions and scale from a describe
// response. The generated getters are nil-safe, so a response missing the
// nested messages yields a zero-valued ScreenDescription rather than panicking.
func screenDescriptionFrom(resp *pb.TargetDescriptionResponse) ScreenDescription {
	dimensions := resp.GetTargetDescription().GetScreenDimensions()
	return ScreenDescription{
		WidthPoints:  int(dimensions.GetWidthPoints()),
		HeightPoints: int(dimensions.GetHeightPoints()),
		Scale:        dimensions.GetDensity(),
	}
}

func (c *grpcCompanion) SendHID(ctx context.Context, events ...HIDEvent) error {
	stream, err := c.client.Hid(ctx)
	if err != nil {
		return err
	}
	for _, e := range events {
		message, err := hidEventToProto(e)
		if err != nil {
			return err
		}
		if err := stream.Send(message); err != nil {
			return err
		}
	}
	_, err = stream.CloseAndRecv()
	return err
}

// hidEventToProto encodes a neutral HID event as this companion's wire event.
// The companion measures delays and swipe durations in seconds.
func hidEventToProto(e HIDEvent) (*pb.HIDEvent, error) {
	switch e.Kind {
	case HIDKindTouchDown:
		return touchProto(e.X, e.Y, pb.HIDEvent_DOWN), nil
	case HIDKindTouchUp:
		return touchProto(e.X, e.Y, pb.HIDEvent_UP), nil
	case HIDKindKeyDown:
		return keyProto(e.Usage, pb.HIDEvent_DOWN), nil
	case HIDKindKeyUp:
		return keyProto(e.Usage, pb.HIDEvent_UP), nil
	case HIDKindDelay:
		return &pb.HIDEvent{Event: &pb.HIDEvent_Delay{
			Delay: &pb.HIDEvent_HIDDelay{Duration: e.Milliseconds / 1000.0},
		}}, nil
	case HIDKindSwipe:
		return &pb.HIDEvent{Event: &pb.HIDEvent_Swipe{Swipe: &pb.HIDEvent_HIDSwipe{
			Start:    &pb.Point{X: e.FromX, Y: e.FromY},
			End:      &pb.Point{X: e.ToX, Y: e.ToY},
			Duration: e.Seconds,
		}}}, nil
	}
	return nil, fmt.Errorf("unknown HID event kind %d", e.Kind)
}

func touchProto(x, y float64, direction pb.HIDEvent_HIDDirection) *pb.HIDEvent {
	return &pb.HIDEvent{Event: &pb.HIDEvent_Press{Press: &pb.HIDEvent_HIDPress{
		Direction: direction,
		Action: &pb.HIDEvent_HIDPressAction{Action: &pb.HIDEvent_HIDPressAction_Touch{
			Touch: &pb.HIDEvent_HIDTouch{Point: &pb.Point{X: x, Y: y}},
		}},
	}}}
}

func keyProto(usage uint32, direction pb.HIDEvent_HIDDirection) *pb.HIDEvent {
	return &pb.HIDEvent{Event: &pb.HIDEvent_Press{Press: &pb.HIDEvent_HIDPress{
		Direction: direction,
		Action: &pb.HIDEvent_HIDPressAction{Action: &pb.HIDEvent_HIDPressAction_Key{
			Key: &pb.HIDEvent_HIDKey{Keycode: uint64(usage)},
		}},
	}}}
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

// installChunkBytes keeps each install payload frame comfortably under the
// companion's 16MiB incoming-message cap.
const installChunkBytes = 4 * 1024 * 1024

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
	// The companion caps incoming messages at 16MiB, so the archive streams in
	// chunks; the companion concatenates consecutive data payloads.
	for _, chunk := range payloadChunks(archive, installChunkBytes) {
		if err := stream.Send(&pb.InstallRequest{Value: &pb.InstallRequest_Payload{
			Payload: &pb.Payload{Source: &pb.Payload_Data{Data: chunk}},
		}}); err != nil {
			return err
		}
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

// payloadChunks splits data into consecutive slices of at most chunkBytes.
func payloadChunks(data []byte, chunkBytes int) [][]byte {
	var chunks [][]byte
	for offset := 0; offset < len(data); offset += chunkBytes {
		end := min(offset+chunkBytes, len(data))
		chunks = append(chunks, data[offset:end])
	}
	return chunks
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
