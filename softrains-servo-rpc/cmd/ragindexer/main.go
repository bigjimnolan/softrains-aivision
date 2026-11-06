package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/fs"
	"net/rpc"
	"os"
	"path/filepath"
	"softrains-servo-rpc/internal/rag"
	"strings"

	"github.com/sirupsen/logrus"
)

type SaveDataRequest struct {
	ID      string
	Content string
}

type GenericReply struct {
	Ok      bool
	Message string
}

type QueryDataRequest struct {
	Query string
}

type QueryDataReply struct {
	Ok               bool
	Message          string
	RetrievedContext []string
}

func main() {
	addr := flag.String("addr", "127.0.0.1:9444", "GimbalRPC server address (used when server is running)")
	indexPath := flag.String("index", "/home/bigjim/rag_index", "Local Bleve index path (used for -direct fallback)")
	direct := flag.Bool("direct", false, "Write directly to Bleve index on disk instead of RPC (server must be stopped)")
	chunkSize := flag.Int("chunk-size", 1000, "Max characters per indexed chunk (0 to index whole file as 1 doc)")
	docIDPrefix := flag.String("id", "", "Optional custom document ID prefix (defaults to filename)")
	query := flag.String("query", "", "Optional test query to run after indexing")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <file-or-dir> [file-or-dir...]\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Indexes text/markdown files into the local RAG store (via live RPC or direct Bleve index).")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	files := collectFiles(flag.Args())
	if len(files) == 0 && *query == "" {
		flag.Usage()
		os.Exit(1)
	}

	var rpcClient *rpc.Client
	if !*direct {
		conn, err := tls.Dial("tcp", *addr, &tls.Config{InsecureSkipVerify: true})
		if err == nil {
			defer conn.Close()
			rpcClient = rpc.NewClient(conn)
			fmt.Printf("[+] Connected to live GimbalRPC server at %s\n", *addr)
		} else {
			fmt.Printf("[!] Could not connect to RPC at %s (%v); falling back to direct index at %s\n", *addr, err, *indexPath)
		}
	}

	var engine *rag.Engine
	if rpcClient == nil {
		logger := logrus.New()
		logger.SetLevel(logrus.WarnLevel)
		var err error
		engine, err = rag.NewEngine(*indexPath, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] Failed to open RAG index at %s: %v\n", *indexPath, err)
			os.Exit(1)
		}
		defer engine.Close()
		fmt.Printf("[+] Opened local Bleve index directly at %s\n", *indexPath)
	}

	totalChunks := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] Failed to read %s: %v\n", file, err)
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}

		baseID := *docIDPrefix
		if baseID == "" {
			baseID = filepath.Base(file)
		} else if len(files) > 1 {
			baseID = fmt.Sprintf("%s-%s", *docIDPrefix, filepath.Base(file))
		}

		chunks := chunkText(content, *chunkSize)
		for i, chunk := range chunks {
			chunkID := baseID
			if len(chunks) > 1 {
				chunkID = fmt.Sprintf("%s#chunk-%d", baseID, i+1)
			}

			if rpcClient != nil {
				var reply GenericReply
				err := rpcClient.Call("GimbalRPC.SaveData", SaveDataRequest{
					ID:      chunkID,
					Content: chunk,
				}, &reply)
				if err != nil || !reply.Ok {
					fmt.Fprintf(os.Stderr, "[-] Failed to index %s via RPC: err=%v msg=%s\n", chunkID, err, reply.Message)
					continue
				}
			} else {
				if err := engine.IndexDocument(rag.Document{
					ID:      chunkID,
					Content: chunk,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "[-] Failed to index %s directly: %v\n", chunkID, err)
					continue
				}
			}
			totalChunks++
			fmt.Printf("[✓] Indexed %s (%d bytes)\n", chunkID, len(chunk))
		}
	}

	if len(files) > 0 {
		fmt.Printf("\n[✓] Finished indexing %d chunk(s) from %d file(s).\n", totalChunks, len(files))
	}

	if *query != "" {
		fmt.Printf("\n[+] Running test query: %q\n", *query)
		if rpcClient != nil {
			var reply QueryDataReply
			if err := rpcClient.Call("GimbalRPC.QueryData", QueryDataRequest{Query: *query}, &reply); err != nil || !reply.Ok {
				fmt.Fprintf(os.Stderr, "[-] Query failed: err=%v msg=%s\n", err, reply.Message)
				os.Exit(1)
			}
			for i, ctx := range reply.RetrievedContext {
				fmt.Printf("--- Result %d ---\n%s\n", i+1, ctx)
			}
		} else {
			results, err := engine.Search(*query)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[-] Search failed: %v\n", err)
				os.Exit(1)
			}
			for i, ctx := range results {
				fmt.Printf("--- Result %d ---\n%s\n", i+1, ctx)
			}
		}
	}
}

func collectFiles(paths []string) []string {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] Skipping %s: %v\n", p, err)
			continue
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			switch ext {
			case ".txt", ".md", ".markdown", ".json", ".csv", ".log", ".yaml", ".yml":
				out = append(out, path)
			}
			return nil
		})
	}
	return out
}

func chunkText(text string, maxChars int) []string {
	if maxChars <= 0 || len(text) <= maxChars {
		return []string{text}
	}

	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var current strings.Builder

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if current.Len() > 0 && current.Len()+len(para)+2 > maxChars {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if len(para) > maxChars {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			for len(para) > maxChars {
				chunks = append(chunks, strings.TrimSpace(para[:maxChars]))
				para = para[maxChars:]
			}
			if strings.TrimSpace(para) != "" {
				current.WriteString(strings.TrimSpace(para))
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
