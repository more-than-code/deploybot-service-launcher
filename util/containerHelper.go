package util

import (
	"context"
	"io"
	"log"
	"os"

	"deploybot-service-agent/model"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type ContainerHelperConfig struct {
}

type ContainerHelper struct {
	cli *client.Client
	cfg ContainerHelperConfig
}

func NewContainerHelper(dockerHost string) *ContainerHelper {
	var cfg ContainerHelperConfig

	cli, err := client.NewClientWithOpts(client.WithHost(dockerHost), client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	return &ContainerHelper{cli: cli, cfg: cfg}
}

func (h *ContainerHelper) StartContainer(cfg *model.DeployConfig) {
	ctx := context.Background()

	h.cli.ContainerStop(ctx, cfg.ServiceName, container.StopOptions{})
	h.cli.ContainerRemove(ctx, cfg.ServiceName, container.RemoveOptions{})

	imageNameTag := cfg.ImageName + ":" + cfg.ImageTag
	reader, err := h.cli.ImagePull(ctx, imageNameTag, image.PullOptions{})
	if err != nil {
		log.Print(err)
		return
	}
	io.Copy(os.Stdout, reader)

	cConfig := &container.Config{
		Image: imageNameTag,
		Env:   cfg.Env,
	}

	if cfg.RestartPolicy.Name == "" {
		cfg.RestartPolicy.Name = "on-failure"
	}

	hConfig := &container.HostConfig{
		AutoRemove:    cfg.AutoRemove,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode(cfg.RestartPolicy.Name), MaximumRetryCount: cfg.RestartPolicy.MaximumRetryCount},
	}

	if cfg.Ports != nil {
		cConfig.ExposedPorts = nat.PortSet{}
		for e := range cfg.Ports {
			cConfig.ExposedPorts[nat.Port(e+"/tcp")] = struct{}{}
		}

		hConfig.PortBindings = nat.PortMap{}
		for e, h := range cfg.Ports {
			hConfig.PortBindings[nat.Port(e+"/tcp")] = []nat.PortBinding{{HostPort: h, HostIP: ""}}
		}
	}

	if cfg.VolumeMounts != nil {
		for s, t := range cfg.VolumeMounts {
			hConfig.Mounts = append(hConfig.Mounts, mount.Mount{Type: mount.TypeBind, Source: s, Target: t})
		}
	}

	nConfig := &network.NetworkingConfig{}

	if cfg.NetworkName != "" && cfg.NetworkId != "" {
		nConfig.EndpointsConfig = map[string]*network.EndpointSettings{cfg.NetworkName: {NetworkID: cfg.NetworkId}}
	}

	resp, err := h.cli.ContainerCreate(ctx, cConfig, hConfig, nConfig, nil, cfg.ServiceName)
	if err != nil {
		log.Print(err)
		return
	}

	if err := h.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		log.Print(err)
		return
	}

	// statusCh, errCh := h.cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)

	// select {
	// case err := <-errCh:
	// 	if err != nil {
	// 		log.Print(err)
	// 		return
	// 	}
	// case <-statusCh:
	// }

	// out, err := h.cli.ContainerLogs(ctx, resp.ID, dTypes.ContainerLogsOptions{ShowStdout: true})
	// if err != nil {
	// 	log.Print(err)
	// 	return
	// }

	// stdcopy.StdCopy(os.Stdout, os.Stderr, out)
}

func (h *ContainerHelper) RestartContainer(cfg *model.RestartConfig) error {
	return h.cli.ContainerRestart(context.Background(), cfg.ServiceName, container.StopOptions{})
}

func (h *ContainerHelper) LogContainer(ctx context.Context, containerName string) (io.ReadCloser, error) {
	return h.cli.ContainerLogs(ctx, containerName, container.LogsOptions{ShowStdout: true})
}

func (h *ContainerHelper) RemoveContainer(ctx context.Context, containerName string) error {
	return h.cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
}

func (h *ContainerHelper) StopContainer(ctx context.Context, containerName string) error {
	return h.cli.ContainerStop(ctx, containerName, container.StopOptions{})
}

func (h *ContainerHelper) CreateNetwork(ctx context.Context, networkName string) (string, error) {
	res, err := h.cli.NetworkCreate(ctx, networkName, types.NetworkCreate{Driver: "bridge"})

	if err != nil {
		return "", err
	}

	return res.ID, nil
}

func (h *ContainerHelper) GetNetworkId(ctx context.Context, networkName string) (string, error) {
	res, err := h.cli.NetworkInspect(ctx, networkName, types.NetworkInspectOptions{})
	if err != nil {
		return "", err
	}

	return res.ID, nil
}

func (h *ContainerHelper) RemoveNetwork(ctx context.Context, networkName string) error {
	return h.cli.NetworkRemove(ctx, networkName)
}

func (h *ContainerHelper) RemoveImages(ctx context.Context) error {
	images, err := h.cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return err
	}

	for _, img := range images {
		items, err := h.cli.ImageRemove(ctx, img.ID, image.RemoveOptions{Force: true, PruneChildren: true})

		if err != nil {
			return err
		}

		log.Println(items)
	}

	return nil
}

func (h *ContainerHelper) RemoveBuilderCache(ctx context.Context) error {
	report, err := h.cli.BuildCachePrune(ctx, types.BuildCachePruneOptions{})
	if err != nil {
		return err
	}

	log.Println(report)

	return nil
}
