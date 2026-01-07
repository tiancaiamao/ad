package main

import (
	"log"

	"github.com/sminez/ad/win/pkg/ad"
)

func main() {
	log.Println("1. Connecting to ad...")
	client, err := ad.NewClient()
	if err != nil {
		log.Fatalf("unable to connect: %v", err)
	}
	defer client.Close()
	log.Println("   ✓ Connected")

	log.Println("2. Opening new window...")
	bufferID, err := client.OpenInNewWindow("+minimal-test")
	if err != nil {
		log.Fatalf("unable to create window: %v", err)
	}
	log.Printf("   ✓ Created buffer %s", bufferID)

	log.Println("3. First write to buffer...")
	if err := client.WriteBody(bufferID, "Line 1\n"); err != nil {
		log.Fatalf("first write failed: %v", err)
	}
	log.Println("   ✓ First write OK")

	log.Println("4. Second write to buffer...")
	if err := client.WriteBody(bufferID, "Line 2\n"); err != nil {
		log.Fatalf("second write failed: %v", err)
	}
	log.Println("   ✓ Second write OK")

	log.Println("5. Third write to buffer...")
	if err := client.WriteBody(bufferID, "Line 3\n"); err != nil {
		log.Fatalf("third write failed: %v", err)
	}
	log.Println("   ✓ Third write OK")

	log.Println("\n✅ All writes successful! Check ad for buffer content.")
	log.Println("Press Ctrl+C to exit...")

	select {}
}
