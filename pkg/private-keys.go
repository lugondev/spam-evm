package pkg

import (
	"bufio"
	"os"
)

// ReadPrivateKeysFromFile reads private keys from the specified file path
func ReadPrivateKeysFromFile(filePath string) []string {
	// open the file
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	// read the file line by line
	scanner := bufio.NewScanner(file)
	var keys []string
	for scanner.Scan() {
		keys = append(keys, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil
	}

	return keys
}
