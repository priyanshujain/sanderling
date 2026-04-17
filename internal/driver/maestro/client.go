package maestro

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/priyanshujain/uatu/internal/driver"
	driverpb "github.com/priyanshujain/uatu/proto/driverpb"
)

type Client struct {
	connection *grpc.ClientConn
	stub       driverpb.DriverClient
}

// Dial connects to the sidecar gRPC server at the given address.
// Address must be a host:port pair, typically "127.0.0.1:<sidecar-port>".
func Dial(address string) (*Client, error) {
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial sidecar: %w", err)
	}
	return &Client{connection: connection, stub: driverpb.NewDriverClient(connection)}, nil
}

func (c *Client) Close() error { return c.connection.Close() }

// WaitForHealth polls the sidecar's Health RPC until it returns Ready=true
// or the context is canceled.
func (c *Client) WaitForHealth(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	for {
		response, err := c.stub.Health(ctx, &driverpb.Empty{})
		if err == nil && response.GetReady() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (c *Client) Launch(ctx context.Context, bundleID, launcherActivity string, clearState bool) error {
	_, err := c.stub.Launch(ctx, &driverpb.LaunchRequest{
		BundleId:         bundleID,
		ClearState:       clearState,
		LauncherActivity: launcherActivity,
	})
	return err
}

func (c *Client) Terminate(ctx context.Context) error {
	_, err := c.stub.Terminate(ctx, &driverpb.Empty{})
	return err
}

func (c *Client) Tap(ctx context.Context, x, y int) error {
	_, err := c.stub.Tap(ctx, &driverpb.Point{X: int32(x), Y: int32(y)})
	return err
}

func (c *Client) TapSelector(ctx context.Context, selector string) error {
	_, err := c.stub.TapSelector(ctx, &driverpb.Selector{Value: selector})
	return err
}

func (c *Client) InputText(ctx context.Context, text string) error {
	_, err := c.stub.InputText(ctx, &driverpb.Text{Value: text})
	return err
}

func (c *Client) Hierarchy(ctx context.Context) (string, error) {
	response, err := c.stub.Hierarchy(ctx, &driverpb.Empty{})
	if err != nil {
		return "", err
	}
	return response.GetJson(), nil
}

func (c *Client) Screenshot(ctx context.Context) (driver.Image, error) {
	response, err := c.stub.Screenshot(ctx, &driverpb.Empty{})
	if err != nil {
		return driver.Image{}, err
	}
	return driver.Image{
		PNG:    response.GetPng(),
		Width:  int(response.GetWidth()),
		Height: int(response.GetHeight()),
	}, nil
}

func (c *Client) WaitForIdle(ctx context.Context, duration time.Duration) error {
	_, err := c.stub.WaitForIdle(ctx, &driverpb.Duration{Millis: duration.Milliseconds()})
	return err
}

func (c *Client) Health(ctx context.Context) (driver.Health, error) {
	response, err := c.stub.Health(ctx, &driverpb.Empty{})
	if err != nil {
		return driver.Health{}, err
	}
	return driver.Health{
		Ready:    response.GetReady(),
		Version:  response.GetVersion(),
		Platform: response.GetPlatform(),
	}, nil
}

var _ driver.Driver = (*Client)(nil)
