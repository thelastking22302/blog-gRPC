package main

import (
	"context"
	"io"
	"log"
	pb "thelastking/blog/pb"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func connectWebSocket() {
	url := "ws://localhost:8080/ws"
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		log.Printf("Received: %s", message)
	}
}

func main() {
	cc, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err.Error())
	}
	defer cc.Close()

	c := pb.NewBlogServiceClient(cc)

	// create a blog
	// createBlog(c)

	// read a blog
	// readBlog(c)

	// update a blog
	// updateBlog(c)

	// delete a blog
	// deleteBlog(c)

	// list blogs
	listBlogs(c)

	// Start WebSocket client
	go connectWebSocket()

	// Keep the main goroutine running
	select {}
}

func createBlog(c pb.BlogServiceClient) {
	req := &pb.CreateBlogRequest{
		Blog: &pb.Blog{
			AuthorId: "Thelastking1",
			Title:    "My First Blog",
			Content:  "Content of the first blog",
		},
	}
	res, err := c.CreateBlog(context.Background(), req)
	if err != nil {
		s, ok := status.FromError(err)
		if ok {
			log.Print(s.Code(), ": ", s.Message())
		} else {
			log.Fatal(err.Error())
		}
	}
	log.Printf("blog created: %v", res.GetBlog())
}

func readBlog(c pb.BlogServiceClient) {
	req := &pb.ReadBlogRequest{
		BlogId: "62fcaacf410e7788bd475335",
	}

	res, err := c.ReadBlog(context.Background(), req)
	if err != nil {
		s, ok := status.FromError(err)
		if ok {
			log.Print(s.Code(), ": ", s.Message())
		} else {
			log.Fatal(err.Error())
		}
	}

	log.Printf("blog found: %v", res.GetBlog())
}

func updateBlog(c pb.BlogServiceClient) {
	req := &pb.UpdateBlogRequest{
		Blog: &pb.Blog{
			Id:       "62fcaacf410e7788bd475335",
			AuthorId: "thelastking",
			Title:    "client update",
			Content:  "New content",
		},
	}
	res, err := c.UpdateBlog(context.Background(), req)
	if err != nil {
		s, ok := status.FromError(err)
		if ok {
			log.Print(s.Code(), ": ", s.Message())
		} else {
			log.Fatal(err.Error())
		}
	}

	log.Printf("blog updated: %v", res.GetBlog())
}

func deleteBlog(c pb.BlogServiceClient) {
	req := &pb.DeleteBlogRequest{
		BlogId: "62fcaacf410e7788bd475335",
	}
	res, err := c.DeleteBlog(context.Background(), req)
	if err != nil {
		s, ok := status.FromError(err)
		if ok {
			log.Print(s.Code(), ": ", s.Message())
		} else {
			log.Fatal(err.Error())
		}
	}

	log.Printf("blog deleted: %s", res.GetBlogId())
}

func listBlogs(c pb.BlogServiceClient) {
	stream, err := c.ListBlog(context.Background(), &pb.ListBlogRequest{})
	if err != nil {
		log.Fatal(err.Error())
	}

	for {
		res, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			s, ok := status.FromError(err)
			if ok {
				log.Print(s.Code(), ": ", s.Message())
			} else {
				log.Fatal(err.Error())
			}
		}
		log.Printf("blog received: %v", res.GetBlog())
	}
}
