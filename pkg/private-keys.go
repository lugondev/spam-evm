package pkg

import (
	"bufio"
	"log"
	"os"
)

const fileName = "private-keys.txt"

// ReadPrivateKeys reads private keys from a file
func ReadPrivateKeys() []string {
	// open the file
	file, err := os.Open(fileName)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// read the file line by line
	scanner := bufio.NewScanner(file)
	var keys []string
	for scanner.Scan() {
		keys = append(keys, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	return keys
}
