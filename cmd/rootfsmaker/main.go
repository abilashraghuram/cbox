package main

import (
	"fmt"
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"

	"github.com/urfave/cli/v2"
	"gvisor.dev/gvisor/pkg/cleanup"
)

const (
	outputDir           = "out"
	workingDir          = outputDir + "/rootfsmaker-working-dir"
	dockerFile          = "./resources/scripts/rootfs/Dockerfile"
	dockerImageName     = "cbox-lambda-img"
	dockerContainerName = "cbox-lambda"
	rootfsTar           = outputDir + "/rootfs.tar"
	rootfsDir           = outputDir + "/rootfs"
	mountDir            = outputDir + "/mnt"
	// We will be using `truncate` to create the image file, this won't really consume all the disk
	// space.
	diskSizeInMB = 4096
)

// runCmd runs `cmdName` with `args`.
func runCmd(cmdName string, args ...string) error {
	cmd := exec.Command(cmdName, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createRootfsFromDockerfile(dockerFile string, outputFile string) (retErr error) {
	cleanup := cleanup.Make(func() {
		if retErr == nil {
			log.Info("create rootfs from docker file finished")
		}
	})

	defer func() {
		cleanup.Clean()
	}()

	log.Info("creating working dir")
	err := runCmd("mkdir", "-p", workingDir)
	if err != nil {
		return fmt.Errorf("failed to create working dir: %s: %w", workingDir, err)
	}
	cleanup.Add(func() {
		if err := runCmd("rm", "-rf", workingDir); err != nil {
			log.WithError(err).Errorf("failed to cleanup working dir: %s", workingDir)
		}
	})

	log.Info("copying dockerfile")
	srcDockerfile := dockerFile
	dstDockerfile := workingDir + "/Dockerfile"
	err = runCmd("cp", srcDockerfile, dstDockerfile)
	if err != nil {
		return fmt.Errorf(
			"failed to copy docker file: %s to: %s: %w",
			srcDockerfile,
			dstDockerfile,
			err,
		)
	}
	cleanup.Add(func() {
		if err := runCmd("rm", "-rf", dstDockerfile); err != nil {
			log.WithError(err).Errorf(
				"failed to cleanup docker file: %s",
				dstDockerfile,
			)
		}
	})

	log.Info("creating output folder")
	err = runCmd("mkdir", "-p", outputDir)
	if err != nil {
		return fmt.Errorf("failed to create output folder: %w", err)
	}

	log.Info("building docker image")
	err = runCmd("docker", "build", "-f", dstDockerfile, "-t", dockerImageName, ".")
	if err != nil {
		return fmt.Errorf("failed to build docker container image: %w", err)
	}
	cleanup.Add(func() {
		if err := runCmd("docker", "rmi", dockerImageName); err != nil {
			log.WithError(err).Errorf("failed to cleanup docker image: %s", dockerImageName)
		}
	})

	// Needed in case the container wasn't cleaned up from previous runs.
	log.Info("removing stale container")
	err = runCmd("docker", "rm", "-f", dockerContainerName)
	if err != nil {
		log.WithError(err).Errorf("failed to remove stale container: %s", dockerContainerName)
	}

	log.Info("creating container")
	err = runCmd("docker", "create", "--name", dockerContainerName, dockerImageName)
	if err != nil {
		return fmt.Errorf("failed to build docker container: %w", err)
	}
	cleanup.Add(func() {
		if err := runCmd("docker", "rm", dockerContainerName); err != nil {
			log.WithError(err).Errorf(
				"failed to cleanup docker container: %s",
				dockerContainerName,
			)
		}
	})

	log.Info("exporting rootfs to tar file")
	err = runCmd("docker", "export", "--output", rootfsTar, dockerContainerName)
	if err != nil {
		return fmt.Errorf("failed to export docker container: %w", err)
	}

	log.Info("creating img file")
	err = runCmd(
		"dd",
		"if=/dev/zero",
		"of="+outputFile,
		"bs=1M",
		"count="+fmt.Sprintf("%d", diskSizeInMB),
	)
	if err != nil {
		return fmt.Errorf("failed to create img file: %w", err)
	}

	log.Info("formatting img file to ext4")
	err = runCmd("mkfs.ext4", outputFile)
	if err != nil {
		return fmt.Errorf("failed to format ext4 image: %w", err)
	}

	log.Info("creating loopback mount directory")
	err = runCmd("mkdir", "-p", mountDir)
	if err != nil {
		return fmt.Errorf("failed to create loopback mount dir: %w", err)
	}
	cleanup.Add(func() {
		if err := runCmd("rm", "-rf", mountDir); err != nil {
			log.WithError(err).Errorf("failed to cleanup mount dir: %s", mountDir)
		}
	})

	log.Info("mounting loopback mount")
	err = runCmd("mount", "-o", "loop", outputFile, mountDir)
	if err != nil {
		return fmt.Errorf("failed to mount ext4 image: %w", err)
	}
	cleanup.Add(func() {
		if err := runCmd("umount", mountDir); err != nil {
			log.WithError(err).Errorf("failed to umount dir: %s", mountDir)
			// Only process to `resize2fs` if umount succeeded.
			return
		}
	})

	log.Info("extracting rootfs tar to mount dir")
	err = runCmd("tar", "-xvf", rootfsTar, "-C", mountDir)
	if err != nil {
		return fmt.Errorf("failed to extract rootfs tar to mount dir: %w", err)
	}
	cleanup.Add(func() {
		if err := runCmd("rm", "-rf", rootfsTar); err != nil {
			log.WithError(err).Errorf("failed to delete rootfs tar: %s", rootfsTar)
		}
	})

	return nil
}

func main() {
	log.Info("making guest rootfs")

	app := &cli.App{
		Name:  "cbox-rootfsmaker",
		Usage: "A script to make a guest rootfs for a VM",
		Commands: []*cli.Command{
			{
				Name:  "create",
				Usage: "Create guest rootfs from docker file",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "dockerfile",
						Aliases:  []string{"d"},
						Usage:    "Path to the docker file",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "output",
						Aliases:  []string{"o"},
						Usage:    "Path to the output rootfs file",
						Required: true,
					},
				},
				Action: func(ctx *cli.Context) error {
					return createRootfsFromDockerfile(ctx.String("dockerfile"), ctx.String("output"))
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.WithError(err).Fatal("failed to create rootfs")
	}
}
