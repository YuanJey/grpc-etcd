// registry/etcd_registry.go
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/resolver"
)

// ServiceInfo 服务信息结构体
type ServiceInfo struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Version string `json:"version"`
}

// EtcdRegistry etcd注册中心结构体
type EtcdRegistry struct {
	client *clientv3.Client
	lease  clientv3.Lease
	ttl    int64
	kv     clientv3.KV
	ctx    context.Context
	cancel context.CancelFunc
}

// NewEtcdRegistry 创建etcd注册中心实例
func NewEtcdRegistry(endpoints []string, ttl int64) (*EtcdRegistry, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	registry := &EtcdRegistry{
		client: cli,
		lease:  clientv3.NewLease(cli),
		ttl:    ttl,
		kv:     clientv3.NewKV(cli),
		ctx:    ctx,
		cancel: cancel,
	}

	return registry, nil
}

// Register 注册服务
func (r *EtcdRegistry) Register(serviceName, address, version string) error {
	serviceInfo := &ServiceInfo{
		Name:    serviceName,
		Address: address,
		Version: version,
	}

	value, err := json.Marshal(serviceInfo)
	if err != nil {
		return err
	}

	// 创建租约
	grantResp, err := r.lease.Grant(r.ctx, r.ttl)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("/services/%s/%s", serviceName, address)

	// 注册服务，带租约
	_, err = r.kv.Put(r.ctx, key, string(value), clientv3.WithLease(grantResp.ID))
	if err != nil {
		return err
	}

	// 启动心跳保持租约
	go r.keepAlive(grantResp.ID, key, string(value))

	return nil
}

// keepAlive 保持租约活跃
func (r *EtcdRegistry) keepAlive(leaseID clientv3.LeaseID, key, value string) {
	// 启动keepalive
	keepAliveChan, err := r.lease.KeepAlive(r.ctx, leaseID)
	if err != nil {
		fmt.Printf("keep alive error: %v\n", err)
		return
	}

	// 监听keepalive响应
	for {
		select {
		case <-r.ctx.Done():
			return
		case resp := <-keepAliveChan:
			if resp == nil {
				// 租约已过期，尝试重新注册
				fmt.Printf("lease expired, try to re-register service %s\n", key)
				// 这里可以添加重新注册逻辑
				return
			}
		}
	}
}

// Unregister 注销服务
func (r *EtcdRegistry) Unregister(serviceName, address string) error {
	key := fmt.Sprintf("/services/%s/%s", serviceName, address)
	_, err := r.kv.Delete(r.ctx, key)
	return err
}

// Discover 发现服务
func (r *EtcdRegistry) Discover(serviceName string) ([]*ServiceInfo, error) {
	prefix := fmt.Sprintf("/services/%s/", serviceName)
	resp, err := r.kv.Get(r.ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	var services []*ServiceInfo
	for _, kv := range resp.Kvs {
		var service ServiceInfo
		if err := json.Unmarshal(kv.Value, &service); err != nil {
			continue
		}
		services = append(services, &service)
	}

	return services, nil
}

// Watch 监听服务变化
func (r *EtcdRegistry) Watch(serviceName string, callback func([]*ServiceInfo)) error {
	prefix := fmt.Sprintf("/services/%s/", serviceName)

	go func() {
		watchChan := r.client.Watch(r.ctx, prefix, clientv3.WithPrefix())
		for {
			select {
			case <-r.ctx.Done():
				return
			case watchResp := <-watchChan:
				for _, event := range watchResp.Events {
					fmt.Printf("Service changed: %s %s\n", event.Type, event.Kv.Key)
					// 服务变更时重新获取服务列表
					services, err := r.Discover(serviceName)
					if err == nil {
						callback(services)
					}
				}
			}
		}
	}()

	// 初始化获取一次服务列表
	services, err := r.Discover(serviceName)
	if err != nil {
		return err
	}
	callback(services)

	return nil
}

// Close 关闭注册中心
func (r *EtcdRegistry) Close() error {
	r.cancel()
	return r.client.Close()
}

// Build 实现resolver.Builder接口
func (r *EtcdRegistry) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	serviceName := target.Endpoint()
	rsv := &etcdResolver{
		registry: r,
		cc:       cc,
	}

	err := r.Watch(serviceName, func(services []*ServiceInfo) {
		var addrs []resolver.Address
		for _, service := range services {
			addrs = append(addrs, resolver.Address{Addr: service.Address})
		}
		cc.UpdateState(resolver.State{Addresses: addrs})
	})

	return rsv, err
}

// Scheme 实现resolver.Builder接口
func (r *EtcdRegistry) Scheme() string {
	return "etcd"
}

// etcdResolver 实现resolver.Resolver接口
type etcdResolver struct {
	registry *EtcdRegistry
	cc       resolver.ClientConn
}

// ResolveNow 实现resolver.Resolver接口
func (r *etcdResolver) ResolveNow(options resolver.ResolveNowOptions) {}

// Close 实现resolver.Resolver接口
func (r *etcdResolver) Close() {}
