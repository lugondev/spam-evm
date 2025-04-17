import { ethers } from "hardhat";
import { ContractTransactionResponse } from "ethers";

async function main() {
	try {
		console.log("Deploying Disperse contract...");

		// Get the contract factory
		const Disperse = await ethers.getContractFactory("Disperse");

		// Deploy the contract
		const disperse = await Disperse.deploy();
		const tx = (await disperse.deploymentTransaction()) as ContractTransactionResponse;
		const receipt = await tx.wait();
		console.info(`Completed at txHash: ${receipt?.hash}`);

		// Get the deployment address
		const address = await disperse.getAddress();

		console.log(`Disperse contract deployed to: ${address}`);

		return address;
	} catch (error) {
		console.error("Deployment failed:", error);
		process.exitCode = 1;
	}
}

main().catch((error) => {
	console.error(error);
	process.exitCode = 1;
});
