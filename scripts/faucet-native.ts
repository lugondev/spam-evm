import { ethers } from "hardhat";
import fs from "fs";
import deployedContracts from "../contracts_deployed.json";
import { loadConfig } from "../config/hardhat";
import { Disperse__factory } from "../typechain-types";
import { chunk } from "lodash";

async function main() {
	const { keysFile, amountFaucet } = loadConfig();
	// read the private keys from the file
	const keys = fs.readFileSync(keysFile, "utf8").split("\n").filter(Boolean);
	const recipientsList = keys.map((key) => {
		const wallet = new ethers.Wallet(key);
		const address = wallet.address;
		return {
			address,
			amount: amountFaucet,
		};
	});

	// create a wallet faucet
	const [faucetWallet] = await ethers.getSigners();
	// create a contract instance
	const disperseAddress = deployedContracts["Disperse"];
	const disperseContract = Disperse__factory.connect(disperseAddress, faucetWallet);

	const chunkedRecipients = chunk(recipientsList, 500);
	for (const recipients of chunkedRecipients) {
		const totalValue = amountFaucet * BigInt(recipients.length);
		// get the balance of the faucet wallet
		const balance = await faucetWallet.provider.getBalance(faucetWallet.address);
		if (balance < totalValue) {
			console.error(
				`Faucet wallet balance is ${ethers.formatEther(
					balance
				)} ETH, but needs ${ethers.formatEther(totalValue)} ETH`
			);
			return;
		} else {
			console.log(
				`Faucet wallet balance is ${ethers.formatEther(
					balance
				)} ETH, enough to send ${ethers.formatEther(totalValue)} ETH`
			);
		}

		const tx = await disperseContract.disperseEther(
			recipients.map((r) => r.address), recipients.map((r) => r.amount),
			{
				value: totalValue,
			}
		);
		await tx.wait();
		console.log(`Transaction hash: ${tx.hash}`);
	}
}

main().catch((error) => {
	console.error(error);
	process.exitCode = 1;
});

