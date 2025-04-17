import { ethers } from "hardhat";
import { ContractTransactionResponse } from "ethers";
import fs from "fs";
import path from "path";

async function main() {
	try {
		const deployedContracts: Record<string, string> = {};
		const contractNames = ["Disperse", "Multicall3"];

		for (let index = 0; index < contractNames.length; index++) {
			const contractName = contractNames[index];
			console.log(`Deploying ${contractName} contract...`);

			// Get the contract factory
			const contractFactory = await ethers.getContractFactory(contractName);

			// Deploy the contract
			const contract = await contractFactory.deploy();
			const tx = contract.deploymentTransaction() as ContractTransactionResponse;
			const receipt = await tx.wait();
			console.info(`${contractName} deployed at txHash: ${receipt?.hash}`);

			// Get the deployment address
			const address = await contract.getAddress();
			console.log(`${contractName} contract deployed to: ${address}`);
			deployedContracts[contractName] = address;
		}

		// Save deployed contract addresses to JSON file
		const deploymentsPath = path.join(process.cwd(), "contracts_deployed.json");
		await fs.promises.writeFile(
			deploymentsPath,
			JSON.stringify(deployedContracts, null, 2),
			"utf8"
		);
		console.log(`\nDeployment addresses saved to: ${deploymentsPath}`);

	} catch (error) {
		console.error("Deployment failed:", error);
		process.exitCode = 1;
	}
}

main().catch((error) => {
	console.error(error);
	process.exitCode = 1;
});
