import { ethers } from "hardhat";
import fs from "fs";
import deployedContracts from "../contracts_deployed.json";
import { loadConfig } from "../config/hardhat";
import { Disperse__factory, Token__factory } from "../typechain-types";
import { chunk } from "lodash";

async function main() {
	const amountFaucet = BigInt(1e18); // 1 token
	const { keysFile } = loadConfig();
	// read the private keys from the file
	let keys = fs.readFileSync(keysFile, "utf8").split("\n").filter(Boolean);
	// remove duplicates
	keys = [...new Set(keys)];
	if (keys.length === 0) {
		console.error("No private keys found in the file");
		return;
	}
	const recipientsList = keys.map((key) => {
		const wallet = new ethers.Wallet(key);
		const address = wallet.address;
		return {
			address,
			amount: amountFaucet
		};
	});

	// create a wallet faucet
	const [faucetWallet] = await ethers.getSigners();
	// create a contract instance
	const disperseAddress = deployedContracts["Disperse"];
	const disperseContract = Disperse__factory.connect(disperseAddress, faucetWallet);
	const tokenAddress = deployedContracts["Token"];
	const tokenContract = Token__factory.connect(tokenAddress, faucetWallet);
	const approveTx = await tokenContract.approve(
		disperseAddress,
		ethers.MaxUint256
	);
	await approveTx.wait();

	const chunkedRecipients = chunk(recipientsList, 500);
	console.log(`Total recipients: ${recipientsList.length}`);
	console.log(`Chunk size: 500`);
	console.log(`Total chunks: ${chunkedRecipients.length}`);

	for (const recipients of chunkedRecipients) {
		const totalValue = amountFaucet * BigInt(recipients.length);
		// get the balance of the faucet wallet
		const balance = await tokenContract.balanceOf(faucetWallet.address);
		if (balance < totalValue) {
			console.error(
				`Faucet wallet balance is ${ethers.formatEther(
					balance
				)} tokens, but needs ${ethers.formatEther(totalValue)} tokens`
			);
			return;
		} else {
			console.log(
				`Faucet wallet balance is ${ethers.formatEther(
					balance
				)} tokens, enough to send ${ethers.formatEther(totalValue)} tokens`
			);
		}

		const tx = await disperseContract.disperseTokenSimple(
			tokenAddress,
			recipients.map((r) => r.address), recipients.map((r) => r.amount),
		);
		await tx.wait();
		console.log(`Transaction hash: ${tx.hash}`);
	}
}

main().catch((error) => {
	console.error(error);
	process.exitCode = 1;
});

