<p align="center">
  <a href="https://github.com/dalmatheo/procon2-driver/commits/main/"><img src="https://img.shields.io/github/last-commit/dalmatheo/procon2-driver?style=for-the-badge"></a>
  <a href="https://github.com/dalmatheo/procon2-driver/releases/latest"><img src="https://img.shields.io/github/v/release/dalmatheo/procon2-driver?style=for-the-badge"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-00ADD8?logo=Go&logoColor=white&style=for-the-badge"></a>
  <a href="https://en.wikipedia.org/wiki/MIT_License"><img src="https://img.shields.io/badge/License-MIT-red?style=for-the-badge"></a>
</p>

<h2 align="center">An unofficial Nintendo Switch™ 2 Pro Controller driver.</h2>

## Table of Contents

- [About](#rocket-about)
- [License](#page_with_curl-license)
- [Install procon2-driver](#arrow_down-install-procon2-driver)
- [How to Build](#construction-how-to-build)

## :rocket: About

**procon2-driver** is a driver that allows users to connect and utilize the **Nintendo Switch 2 Pro Controller** on the Linux operating system.

This project is founded by **[@dalmatheo](https://github.com/dalmatheo)**. The core goals of this project are:

- **Compatibility**: To provide seamless and stable support for the Pro Controller 2 across any game, using the uinput of linux.
- **Performance**: To ensure the driver has a minimal impact on system resources and maintains **low-latency** communication for a reliable gaming experience.

## :page_with_curl: License

This project is available under the **[MIT license](https://en.wikipedia.org/wiki/MIT_License)**. You can review the full license agreement at the following link: [The MIT License](https://opensource.org/license/mit).

## :arrow_down: Install procon2-driver

### Install it using the provided script

You can install the package using the [provided script](https://github.com/dalmatheo/procon2-driver/blob/main/install.sh):

```shell
sh -c "$(curl -fsSL https://raw.githubusercontent.com/dalmatheo/procon2-driver/refs/heads/main/install.sh)"
```

### Install it from a local build

You can [build the package](#construction-how-to-build) to then install it by using this command:

```shell
chmod +x ./deploy.sh
./deploy.sh
```

## :construction: How to Build

To build the project, you'll have to:

### Install Go

Go is a simple low level programming language. It's the language that was used for the project.

Here's how to install it on my distribution (Arch)

```shell
sudo pacman -S go
```

### Clone the repository

Then, you'll have to clone the repository to get the source code on your machine and you'll have to open the directory in your terminal.

```shell
git clone https://github.com/dalmatheo/procon2-driver
cd procon2-driver
```

### Build the project

Finally, you'll have to execute the command to build the project.

```shell
go build
```

If you want to build and install the driver, use:

```shell
chmod +x ./deploy.sh
./deploy.sh
```
