import { ethers } from "hardhat";
import fs from "fs";
import deployedContracts from "../contracts_deployed.json";
import { loadConfig } from "../config/hardhat";
import { Disperse__factory } from "../typechain-types";

async function main() {
	const { keysFile, amountFaucet } = loadConfig();
	// read the private keys from the file
	const keys = fs.readFileSync(keysFile, "utf8").split("\n").filter(Boolean);
	const recipients = keys.map((key) => {
		const wallet = new ethers.Wallet(key);
		const address = wallet.address;
		return {
			address,
			amount: amountFaucet,
		};
	});
	const totalValue = amountFaucet * BigInt(recipients.length);

	// create a wallet faucet
	const [faucetWallet] = await ethers.getSigners();
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

	// create a contract instance
	const disperseAddress = deployedContracts["Disperse"];
	const disperseContract = Disperse__factory.connect(disperseAddress, faucetWallet);
	// loop through the keys and send 0.01 ETH to each address
	const tx = await disperseContract.disperseEther(
		recipients.map((r) => r.address), recipients.map((r) => r.amount),
		{
			value: totalValue,
		}
	);
	await tx.wait();
	console.log(`Transaction hash: ${tx.hash}`);
}

main().catch((error) => {
	console.error(error);
	process.exitCode = 1;
});

