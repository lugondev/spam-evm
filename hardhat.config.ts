import { HardhatUserConfig } from "hardhat/config";
import "@nomicfoundation/hardhat-toolbox";
import "dotenv/config";

const PRIVATE_KEY = process.env.PRIVATE_KEY!!;
const RPC_URL = process.env.RPC_URL!!;

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
