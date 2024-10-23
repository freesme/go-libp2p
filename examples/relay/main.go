package main

import (
	"context"
	"log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	ma "github.com/multiformats/go-multiaddr"
)

func main() {
	run()
}

func run() {
	// Create two "unreachable" libp2p hosts that want to communicate.
	// We are configuring them with no listen addresses to mimic hosts
	// that cannot be directly dialed due to problematic firewall/NAT
	// configurations.

	// 创建两个想要通信的“不可达”libp2p主机，我们配置它们没有监听地址来模拟主机，由于防火墙/NAT配置有问题而无法直接拨打。
	unreachable1, err := libp2p.New(
		libp2p.NoListenAddrs,
		// Usually EnableRelay() is not required as it is enabled by default
		// but NoListenAddrs overrides this, so we're adding it in explicitly again.
		// 通常EnableRelay（）不是必需的，因为它是默认启用的，但nolistenaddr覆盖了它，所以我们再次显式添加它。
		libp2p.EnableRelay(),
	)
	if err != nil {
		log.Printf("Failed to create unreachable1: %v", err)
		return
	}

	log.Println("PEER INFO:PEER1 ID:", unreachable1.ID(), "ADDR:", unreachable1.Addrs())

	unreachable2, err := libp2p.New(
		libp2p.NoListenAddrs,
		libp2p.EnableRelay(),
	)

	if err != nil {
		log.Printf("Failed to create unreachable2: %v", err)
		return
	}

	log.Println("First let's attempt to directly connect")

	// Attempt to connect the unreachable hosts directly
	// 尝试直接连接不可达的主机

	log.Println("PEER INFO:PEER2 ID:", unreachable2.ID(), "ADDR:", unreachable2.Addrs())

	unreachable2info := peer.AddrInfo{
		ID:    unreachable2.ID(),
		Addrs: unreachable2.Addrs(),
	}

	err = unreachable1.Connect(context.Background(), unreachable2info)
	if err == nil {
		log.Printf("This actually should have failed.")
		return
	}
	// 正如我们猜想的那样，我们无法在无法到达的主机之间直接拨号
	log.Println("As suspected we cannot directly dial between the unreachable hosts")

	// 创建一个主机作为中间人，代表我们传递消息
	relay1, err := libp2p.New()
	if err != nil {
		log.Printf("Failed to create relay1: %v", err)
		return
	}

	// Configure the host to offer the circuit relay service.
	// Any host that is directly dialable in the network (or on the internet)
	// can offer a circuit relay service, this isn't just the job of
	// "dedicated" relay services.
	// In circuit relay v2 (which we're using here!) it is rate limited so that
	// any node can offer this service safely

	// 配置主机以提供电路中继服务。
	// 任何可以在网络（或互联网）中直接拨号的主机都可以提供电路中继服务，这不仅仅是“专用”中继服务的工作。
	// 在电路继电器v2（我们在这里使用！）中，它的速率是有限的，因此任何节点都可以安全地提供此服务
	_, err = relay.New(relay1)
	if err != nil {
		log.Printf("Failed to instantiate the relay: %v", err)
		return
	}

	relay1info := peer.AddrInfo{
		ID:    relay1.ID(),
		Addrs: relay1.Addrs(),
	}

	// 将unreachable1和unreachable2都连接到relay1
	if err := unreachable1.Connect(context.Background(), relay1info); err != nil {
		log.Printf("Failed to connect unreachable1 and relay1: %v", err)
		return
	}

	if err := unreachable2.Connect(context.Background(), relay1info); err != nil {
		log.Printf("Failed to connect unreachable2 and relay1: %v", err)
		return
	}

	// Now, to test the communication, let's set up a protocol handler on unreachable2
	// 现在，为了测试通信，让我们在unreachable2上设置一个协议处理程序
	unreachable2.SetStreamHandler("/customprotocol", func(s network.Stream) {
		log.Println("Awesome! We're now communicating via the relay!")

		// End the example
		s.Close()
	})

	// Hosts that want to have messages relayed on their behalf need to reserve a slot
	// with the circuit relay service host
	// As we will open a stream to unreachable2, unreachable2 needs to make the
	// reservation

	// 希望以自己的身份中继消息的主机需要在中继服务主机上预订一个槽位
	// 由于我们将向unreachable2打开一个流，unreachable2需要进行预订
	_, err = client.Reserve(context.Background(), unreachable2, relay1info)
	if err != nil {
		log.Printf("unreachable2 failed to receive a relay reservation from relay1. %v", err)
		return
	}

	// Now create a new address for unreachable2 that specifies to communicate via
	// relay1 using a circuit relay
	// 现在为unachable2创建一个新地址，指定使用中继服务通过relay1进行通信
	relayaddr, err := ma.NewMultiaddr("/p2p/" + relay1info.ID.String() + "/p2p-circuit/p2p/" + unreachable2.ID().String())
	if err != nil {
		log.Println(err)
		return
	}

	// Since we just tried and failed to dial, the dialer system will, by default
	// prevent us from redialing again so quickly. Since we know what we're doing, we
	// can use this ugly hack (it's on our TODO list to make it a little cleaner)
	// to tell the dialer "no, its okay, let's try this again"
	// 由于我们刚刚拨号失败，默认情况下，拨号系统将阻止我们如此迅速地重新拨号。既然我们知道我们在做什么，
	// 我们可以使用这个丑陋的hack（它在我们的TODO 列表中，使它更简洁）来告诉拨号器“不，没关系，让我们再试一次”。
	unreachable1.Network().(*swarm.Swarm).Backoff().Clear(unreachable2.ID())

	// 尝试使用中继节点连接主机
	log.Println("Now let's attempt to connect the hosts via the relay node")

	// 通过中继地址打开到先前不可达主机的连接
	unreachable2relayinfo := peer.AddrInfo{
		ID:    unreachable2.ID(),
		Addrs: []ma.Multiaddr{relayaddr},
	}
	if err := unreachable1.Connect(context.Background(), unreachable2relayinfo); err != nil {
		log.Printf("Unexpected error here. Failed to connect unreachable1 and unreachable2: %v", err)
		return
	}

	log.Println("Yep, that worked!")

	// Woohoo! we're connected!
	// Let's start talking!

	// Because we don't have a direct connection to the destination node - we have a relayed connection -
	// the connection is marked as transient. Since the relay limits the amount of data that can be
	// exchanged over the relayed connection, the application needs to explicitly opt-in into using a
	// relayed connection. In general, we should only do this if we have low bandwidth requirements,
	// and we're happy for the connection to be killed when the relayed connection is replaced with a
	// direct (holepunched) connection.

	// 因为我们没有直接连接到目标节点，我们有一个中继连接，连接被标记为暂态。
	// 由于中继限制了可以通过中继连接交换的数据量，因此应用程序需要显式地选择使用中继连接。
	// 一般来说，只有在带宽要求较低的情况下，我们才应该这样做，我们很高兴[中继连接]被[直接（holepched）连接]取代时终止。
	s, err := unreachable1.NewStream(network.WithUseTransient(context.Background(), "customprotocol"), unreachable2.ID(), "/customprotocol")
	if err != nil {
		log.Println("Whoops, this should have worked...: ", err)
		return
	}

	s.Read(make([]byte, 1)) // block until the handler closes the stream
}
