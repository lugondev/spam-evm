import { readFileSync } from 'fs';
import { parseUnits } from 'ethers';
import { load } from 'js-yaml';
import { join } from 'path';

interface Config {
	keysFile: string;
	providerUrls: string[];
	faucet: {
		privateKey: string;
		amountPerTransfer: string;
	};
}

export function loadConfig(): { keysFile: string; rpcUrl: string; privateKey: string; amountFaucet: bigint } {
	try {
		const configPath = join(process.cwd(), 'config.yaml');
		const fileContent = readFileSync(configPath, 'utf8');
		const config = load(fileContent) as Config;

		if (!config.providerUrls?.[0]) {
			throw new Error('No RPC URL found in config.yaml');
		}

		if (!config.faucet?.privateKey) {
			throw new Error('No private key found in config.yaml');
		}

		return {
			keysFile: config.keysFile || 'private-keys.txt',
			rpcUrl: config.providerUrls[0],
			privateKey: config.faucet.privateKey,
			amountFaucet: parseUnits(config.faucet.amountPerTransfer || '1'),
		};
	} catch (error) {
		console.error('Error loading config:', error);
		throw error;
	}
}
