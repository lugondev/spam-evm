import { HardhatUserConfig } from "hardhat/config";
import "@nomicfoundation/hardhat-toolbox";
import { loadConfig } from "./config/hardhat";

const { rpcUrl, privateKey } = loadConfig();

const PRIVATE_KEY = privateKey || process.env.PRIVATE_KEY!!;
const RPC_URL = rpcUrl || process.env.RPC_URL!!;


const config: HardhatUserConfig = {
	solidity: "0.8.20",
	networks: {
		devnet: {
			url: RPC_URL,
			accounts: [PRIVATE_KEY],
		},
	},
};

export default config;
