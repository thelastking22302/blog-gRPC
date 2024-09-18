package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"

	pb "thelastking/blog/pb"
	"time"

	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

var collection *mongo.Collection
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins
	},
}

type server struct {
	pb.UnimplementedBlogServiceServer
}

type blogItem struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	AuthorID string             `bson:"author_id"`
	Content  string             `bson:"content"`
	Title    string             `bson:"title"`
}

func (s *server) CreateBlog(ctx context.Context, req *pb.CreateBlogRequest) (*pb.CreateBlogResponse, error) {
	log.Print("CreateBlog invoked")
	blog := req.GetBlog()

	data := blogItem{
		AuthorID: blog.GetAuthorId(),
		Title:    blog.GetTitle(),
		Content:  blog.GetContent(),
	}

	res, err := collection.InsertOne(ctx, data)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "internal error: %s", err.Error())
	}

	oid, ok := res.InsertedID.(primitive.ObjectID)
	if !ok {
		return nil, status.Error(codes.Internal, "cannot convert to object id")
	}

	return &pb.CreateBlogResponse{
		Blog: &pb.Blog{
			Id:       oid.Hex(),
			AuthorId: blog.GetAuthorId(),
			Title:    blog.GetTitle(),
			Content:  blog.GetContent(),
		},
	}, nil
}

// read blog from mongodb
func (s *server) ReadBlog(ctx context.Context, req *pb.ReadBlogRequest) (*pb.ReadBlogResponse, error) {
	log.Print("ReadBlog invoked")
	blodId := req.GetBlogId()

	oid, err := primitive.ObjectIDFromHex(blodId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "cannot parse id")
	}

	var res *blogItem
	err = collection.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&res)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, status.Error(codes.NotFound, "document not found")
		}
		return nil, status.Errorf(codes.Internal, "internal err: %s", err.Error())
	}

	return &pb.ReadBlogResponse{
		Blog: &pb.Blog{
			Id:       res.ID.Hex(),
			AuthorId: res.AuthorID,
			Title:    res.Title,
			Content:  res.Content,
		},
	}, nil
}

// update blog in mongodb
func (s *server) UpdateBlog(ctx context.Context, req *pb.UpdateBlogRequest) (*pb.UpdateBlogResponse, error) {
	log.Print("UpdateBlog invoked")
	blog := req.GetBlog()

	oid, err := primitive.ObjectIDFromHex(blog.GetId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "cannot parse id")
	}

	// find document by id
	_, err = s.ReadBlog(ctx, &pb.ReadBlogRequest{
		BlogId: oid.Hex(),
	})
	if err != nil {
		return nil, err
	}

	// update document
	data := &blogItem{
		ID:       oid,
		AuthorID: blog.AuthorId,
		Content:  blog.Content,
		Title:    blog.Title,
	}

	_, err = collection.ReplaceOne(ctx, bson.D{{Key: "_id", Value: oid}}, data)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "internal err: %s", err.Error())
	}

	return &pb.UpdateBlogResponse{
		Blog: &pb.Blog{
			Id:       oid.Hex(),
			AuthorId: data.AuthorID,
			Title:    data.Title,
			Content:  data.Content,
		},
	}, nil
}

func (s *server) DeleteBlog(ctx context.Context, req *pb.DeleteBlogRequest) (*pb.DeleteBlogResponse, error) {
	log.Print("DeleteBlog invoked")
	blogId := req.GetBlogId()

	oid, err := primitive.ObjectIDFromHex(blogId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "cannot parse id")
	}

	// delete document
	res, err := collection.DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "internal err: %s", err.Error())
	}

	if res.DeletedCount == 0 {
		return nil, status.Error(codes.NotFound, "document not found")
	}

	return &pb.DeleteBlogResponse{
		BlogId: blogId,
	}, nil
}

func (s *server) ListBlog(req *pb.ListBlogRequest, stream pb.BlogService_ListBlogServer) error {
	log.Print("ListBlog invoked")

	cur, err := collection.Find(context.Background(), bson.M{})
	if err != nil {
		return status.Errorf(codes.Internal, "internal err: %s", err.Error())
	}
	defer cur.Close(context.Background())

	// iterate over data
	var data *blogItem
	for cur.Next(context.Background()) {
		err := cur.Decode(&data)
		if err != nil {
			return status.Errorf(codes.Internal, "internal err: %s", err.Error())
		}
		stream.Send(&pb.ListBlogResponse{
			Blog: &pb.Blog{
				Id:       data.ID.Hex(),
				AuthorId: data.AuthorID,
				Title:    data.Title,
				Content:  data.Content,
			},
		})
	}
	if err := cur.Err(); err != nil {
		return status.Errorf(codes.Internal, "internal err: %s", err.Error())
	}
	return nil
}

func  handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	// Watch for changes in the blogs collection
	pipeline := mongo.Pipeline{bson.D{{Key: "$match", Value: bson.D{{Key: "operationType", Value: "insert"}}}}}
	stream, err := collection.Watch(context.Background(), pipeline)
	if err != nil {
		log.Println(err)
		return
	}
	defer stream.Close(context.Background())

	for stream.Next(context.Background()) {
		var changeEvent bson.M
		if err := stream.Decode(&changeEvent); err != nil {
			log.Println(err)
			continue
		}

		fullDocument, ok := changeEvent["fullDocument"].(bson.M)
		if !ok {
			log.Println("Error: fullDocument not found or not a bson.M")
			continue
		}

		blog := &pb.Blog{
			Id:       fullDocument["_id"].(primitive.ObjectID).Hex(),
			AuthorId: fullDocument["author_id"].(string),
			Title:    fullDocument["title"].(string),
			Content:  fullDocument["content"].(string),
		}

		blogJSON, err := json.Marshal(blog)
		if err != nil {
			log.Println(err)
			continue
		}

		if err := conn.WriteMessage(websocket.TextMessage, blogJSON); err != nil {
			log.Println(err)
			return
		}
	}
}

func main() {

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// create mongodb client
	log.Print("connecting to mongodb")
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://thelastking2:220302@localhost:27017"))
	if err != nil {
		log.Fatal(err.Error())
	}

	collection = client.Database("mydb").Collection("blog")

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err.Error())
	}

	s := grpc.NewServer()
	pb.RegisterBlogServiceServer(s, &server{})

	reflection.Register(s)

	go func() {
		log.Print("starting server")
		if err = s.Serve(lis); err != nil {
			log.Fatal(err.Error())
		}
	}()

	// Add WebSocket handler
	http.HandleFunc("/ws", handleWebSocket)

	// Start HTTP server for WebSocket
	go func() {
		log.Println("Starting WebSocket server on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatalf("Failed to start WebSocket server: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch

	log.Print("stopping server")
	s.Stop()

	log.Print("closing listener")
	lis.Close()

	log.Print("closing mongodb connection")
	if err = client.Disconnect(ctx); err != nil {
		panic(err)
	}

	log.Print("bye bye")

}
