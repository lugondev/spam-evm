import { readFileSync } from 'fs';
import { load } from 'js-yaml';
import { join } from 'path';

interface Config {
	providerUrls: string[];
	faucet: {
		privateKey: string;
	};
}

export function loadConfig(): { rpcUrl: string; privateKey: string } {
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
			rpcUrl: config.providerUrls[0],
			privateKey: config.faucet.privateKey,
		};
	} catch (error) {
		console.error('Error loading config:', error);
		throw error;
	}
}
