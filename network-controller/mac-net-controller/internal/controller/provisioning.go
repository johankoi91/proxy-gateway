package controller

import (
	"context"
	"errors"
	"strings"

	"lightnode-mac-controller/internal/model"
)

func (c *Controller) ImportVPSConfig(ctx context.Context, document model.VPSConfig) (model.NodeConfiguration, error) {
	update, err := nodeUpdateFromVPSConfig(document)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	return c.UpdateNode(ctx, update)
}

func nodeUpdateFromVPSConfig(document model.VPSConfig) (model.NodeUpdate, error) {
	if document.Schema != "lightnode.vps-config" || document.Version != 1 {
		return model.NodeUpdate{}, errors.New("不支持的 VPS 配置格式或版本")
	}
	if !document.VLESS.TLS.Enabled || !document.VLESS.TLS.Reality.Enabled {
		return model.NodeUpdate{}, errors.New("VPS 配置必须启用 TLS Reality")
	}
	if document.VLESS.Transport != "tcp" || document.VLESS.Encryption != "none" {
		return model.NodeUpdate{}, errors.New("当前仅支持 VLESS TCP 与 none encryption")
	}
	update := model.NodeUpdate{
		Server: strings.TrimSpace(document.Endpoint.Address), Port: document.Endpoint.Port,
		ExpectedEgress: strings.TrimSpace(document.Endpoint.ExpectedEgress),
		Flow:           document.VLESS.Flow, TLSServerName: document.VLESS.TLS.ServerName,
		UTLSFingerprint: document.VLESS.TLS.UTLSFingerprint,
		UUID:            document.VLESS.UUID, RealityPublicKey: document.VLESS.TLS.Reality.PublicKey,
		RealityShortID: document.VLESS.TLS.Reality.ShortID,
	}
	if err := validateNodeUpdate(update); err != nil {
		return model.NodeUpdate{}, err
	}
	if update.UUID == "" || update.RealityPublicKey == "" || update.RealityShortID == "" {
		return model.NodeUpdate{}, errors.New("VPS 配置缺少 VLESS/Reality 凭据")
	}
	return update, nil
}
