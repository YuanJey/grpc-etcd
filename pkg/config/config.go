package config

var Config config

const ConfName = "youKaConf"

type config struct {
	Etcd struct {
		EtcdSchema string   `yaml:"etcdSchema"`
		EtcdAddr   []string `yaml:"etcdAddr"`
		UserName   string   `yaml:"userName"`
		Password   string   `yaml:"password"`
		Secret     string   `yaml:"secret"`
	}
	RpcRegisterIP   string `yaml:"rpcRegisterIP"`
	RpcRegisterName struct {
		SCUserName string `yaml:"sCUserName"`
		RelayName  string `yaml:"relayName"`
	}
	RpcPort struct {
		SCUserPort  []int `yaml:"sCUserPort"`
		GatewayPort []int `yaml:"gatewayPort"`
	}
}
